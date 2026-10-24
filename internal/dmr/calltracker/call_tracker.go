// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

// Package calltracker records every transmission that the hub hears, of
// every kind, as a row of the calls table.
package calltracker

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/puzpuzpuz/xsync/v4"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type netEventPayload struct {
	NetID       uint   `json:"net_id"`
	TalkgroupID uint   `json:"talkgroup_id"`
	Event       string `json:"event"`
}

const (
	voiceHang       = 2 * time.Second
	controlHang     = time.Second
	dataHang        = time.Second
	dataRowHang     = 3 * time.Second
	responseWindow  = 10 * time.Second
	keyUp           = 100 * time.Millisecond
	sweepInterval   = 50 * time.Millisecond
	channelIdle     = 5 * time.Minute
	peerChannelIdle = 30 * time.Second
	streamIdle      = time.Minute
	shardCount      = 8
	shardBits       = 3
	shardDepth      = 1024
	maxStreamRows   = 32
	maxCallData     = 33 * 10000
	packetTimingMs  = 60
	pct             = 100
	kvTimeout       = time.Second
	recentKeyPrefix = "dmrhub:calltracker:recent:"
	answerKeyPrefix = "dmrhub:calltracker:answer:"
	responsesTopic  = "calltracker:responses"
	dropsTopic      = "calltracker:drops"
)

// DataObserver is told of each data packet and each response that the
// tracker hears. Its methods run on the worker of the logical channel, in
// burst order, and must not block or call Flush or Stop. ev.Message.Payload
// is valid only until the method returns. Take the addresses from the row
// or the message header, not from ev.Call, which is the call that the
// receiver follows and can be the other direction of a reply.
type DataObserver interface {
	// DataPacket is called for each EventDelivered and EventReassembled.
	// call is the row of the data call; its ID is set.
	DataPacket(call *models.Call, ev *data.Event, at time.Time)
	// DataResponse is called for each EventResponse. call is the row of
	// the response.
	DataResponse(call *models.Call, ev *data.Event, at time.Time)
}

// CallTracker records the transmissions that the hub hears. Track queues
// each packet on the worker of its logical channel, so tracking never
// blocks routing, and one worker owns each data.Receiver.
type CallTracker struct {
	db     *gorm.DB
	pubsub pubsub.PubSub
	kv     kv.KV
	epoch  time.Time
	// activeNets maps talkgroup ID to net ID for currently active nets.
	// Updated via pubsub subscription to avoid a DB query per call end.
	activeNets *xsync.Map[uint, uint]
	cancels    []context.CancelFunc

	shards   []*shard
	done     chan struct{}
	stopOnce sync.Once
	stopped  atomic.Bool
	wg       sync.WaitGroup

	streams  *xsync.Map[uint, streamRef]
	sources  *xsync.Map[sourceKey, streamRef]
	recentMu sync.Mutex
	recent   map[pair][]recentRow

	observer atomic.Pointer[observerBox]
	dropped  atomic.Uint64
	metrics  *metrics.Metrics
	bursts   atomic.Uint64
}

type observerBox struct{ o DataObserver }

type streamRef struct {
	shard int
	at    time.Time
}

type pair struct{ src, dst uint }

type recentRow struct {
	shard     int
	row       *row
	kind      models.CallKind
	sub       string
	confirmed bool
	at        time.Time
}

// NewCallTracker creates a new CallTracker and starts its workers. The
// replicas of the hub share kv and pubsub, so that an answer heard on one
// replica is set on the row of the transmission that another one heard.
func NewCallTracker(db *gorm.DB, pubsub pubsub.PubSub, kv kv.KV) *CallTracker {
	ct := &CallTracker{
		db:         db,
		pubsub:     pubsub,
		kv:         kv,
		epoch:      time.Now(),
		activeNets: xsync.NewMap[uint, uint](),
		streams:    xsync.NewMap[uint, streamRef](),
		sources:    xsync.NewMap[sourceKey, streamRef](),
		recent:     map[pair][]recentRow{},
		done:       make(chan struct{}),
		metrics:    metrics.NewMetrics(),
	}
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	ct.bursts.Store(binary.LittleEndian.Uint64(seed[:]) >> (shardBits + 2))
	ct.loadActiveNets()
	ct.subscribe("net:events", ct.netEvent)
	ct.subscribe(responsesTopic, ct.remoteResponse)
	ct.subscribe(dropsTopic, ct.remoteDrop)
	ct.shards = make([]*shard, shardCount)
	for i := range ct.shards {
		ct.shards[i] = newShard(ct, i)
		ct.wg.Add(1)
		go ct.shards[i].run()
	}
	return ct
}

// SetDataObserver sets the observer of data packets and responses. Set it
// before the first Track.
func (c *CallTracker) SetDataObserver(o DataObserver) {
	c.observer.Store(&observerBox{o: o})
}

func (c *CallTracker) dataObserver() DataObserver {
	if b := c.observer.Load(); b != nil {
		return b.o
	}
	return nil
}

// Stop ends every open transmission, stops the workers and cancels the
// pubsub subscriptions.
func (c *CallTracker) Stop() {
	c.stopOnce.Do(func() {
		c.stopped.Store(true)
		for _, s := range c.shards {
			s.in <- task{stop: true}
		}
		c.wg.Wait()
		close(c.done)
		for _, cancel := range c.cancels {
			cancel()
		}
	})
}

// Dropped returns the number of packets that Track dropped because the
// queue of their worker was full.
func (c *CallTracker) Dropped() uint64 {
	return c.dropped.Load()
}

// Track queues the packet p, which the server named server got, to be
// recorded. peer reports a server of network peers, such as OpenBridge,
// whose Repeater field is a peer ID and which carries many streams on one
// slot. Track never blocks: when the queue of the worker is full, the
// packet is not recorded and Dropped counts it. It returns the ID of the
// burst, for p.TrackID, or zero when it is stopped.
func (c *CallTracker) Track(_ context.Context, p models.Packet, server string, peer bool) uint64 {
	if c.stopped.Load() {
		return 0
	}
	key := chanKey{server: server, repeater: p.Repeater, slot: p.Slot}
	if peer {
		key.stream = p.StreamID
	}
	idx := key.shard()
	now := time.Now()
	id := c.bursts.Add(1)<<shardBits | uint64(idx) //nolint:gosec
	c.streams.Store(p.StreamID, streamRef{shard: idx, at: now})
	if !peer {
		c.sources.Store(sourceKey{server: server, repeater: p.Repeater, src: p.Src}, streamRef{shard: idx, at: now})
	}
	c.enqueue(idx, task{key: key, p: p, peer: peer, at: now, id: id})
	return id
}

// Undeliverable records that target, such as "ipsc:311304200:no_ipsc_form"
// (the server, the repeater and the reason), could not
// take the burst of p, a packet that Track got, on the transmission that
// holds the burst. Call it after Track for the same packet. p.TrackID
// names the burst; without it the mark goes on the latest transmission of
// the stream. The hub counts the burst in its metrics.
func (c *CallTracker) Undeliverable(p models.Packet, target string) {
	if c.stopped.Load() {
		return
	}
	idx := int(p.TrackID % shardCount)
	if p.TrackID == 0 {
		ref, ok := c.streams.Load(p.StreamID)
		if !ok {
			return
		}
		idx = ref.shard
	}
	c.enqueue(idx, task{fn: func(s *shard) { s.undeliverable(p.StreamID, p.TrackID, target) }})
}

// Flush waits until the workers handled every packet queued before it.
func (c *CallTracker) Flush() {
	barriers := make([]chan struct{}, len(c.shards))
	for i := range c.shards {
		barriers[i] = make(chan struct{})
		b := barriers[i]
		select {
		case c.shards[i].in <- task{fn: func(*shard) { close(b) }}:
		case <-c.done:
			return
		}
	}
	for _, b := range barriers {
		select {
		case <-b:
		case <-c.done:
			return
		}
	}
}

func (c *CallTracker) enqueue(idx int, t task) {
	select {
	case c.shards[idx].in <- t:
	default:
		c.metrics.IncrementTrackerDropped()
		n := c.dropped.Add(1)
		if n&(n-1) == 0 {
			slog.Warn("CallTracker: queue full, packets not recorded", "dropped", n)
		}
	}
}

func (c *CallTracker) now() time.Duration {
	return time.Since(c.epoch)
}

type chanKey struct {
	server   string
	repeater uint
	slot     bool
	stream   uint
}

func (k chanKey) shard() int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(k.server))
	var b [17]byte
	binary.LittleEndian.PutUint64(b[:8], uint64(k.repeater))
	binary.LittleEndian.PutUint64(b[8:16], uint64(k.stream))
	if k.slot {
		b[16] = 1
	}
	_, _ = h.Write(b[:])
	return int(h.Sum32() % shardCount)
}

type task struct {
	key  chanKey
	p    models.Packet
	peer bool
	at   time.Time
	id   uint64
	fn   func(*shard)
	stop bool
}

func (c *CallTracker) loadActiveNets() {
	var nets []models.Net
	if err := c.db.Where("active = ?", true).Find(&nets).Error; err != nil {
		slog.Error("CallTracker: failed to load active nets", "error", err)
		return
	}
	for i := range nets {
		c.activeNets.Store(nets[i].TalkgroupID, nets[i].ID)
	}
	if len(nets) > 0 {
		slog.Info("CallTracker: loaded active nets into cache", "count", len(nets))
	}
}

func (c *CallTracker) subscribe(topic string, handle func([]byte)) {
	sub := c.pubsub.Subscribe(topic)
	ctx, cancel := context.WithCancel(context.Background())
	c.cancels = append(c.cancels, cancel)
	go func() {
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				if err := sub.Close(); err != nil {
					slog.Error("CallTracker: failed to close a subscription", "topic", topic, "error", err)
				}
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if len(msg) > 0 {
					handle(msg)
				}
			}
		}
	}()
}

func (c *CallTracker) netEvent(msg []byte) {
	var evt netEventPayload
	if err := json.Unmarshal(msg, &evt); err != nil {
		slog.Error("CallTracker: failed to unmarshal net event", "error", err)
		return
	}
	switch evt.Event {
	case "started":
		c.activeNets.Store(evt.TalkgroupID, evt.NetID)
	case "stopped":
		c.activeNets.Delete(evt.TalkgroupID)
	}
}

func (c *CallTracker) remember(s *shard, r *row) {
	if r.call.GroupCall {
		return
	}
	k := pair{src: r.call.SourceID, dst: r.call.DestinationID}
	e := recentRow{shard: s.idx, row: r, kind: r.call.Kind, sub: r.call.SubKind, confirmed: r.call.Confirmed, at: time.Now()}
	c.recentMu.Lock()
	cut := e.at.Add(-responseWindow)
	keep := c.recent[k][:0]
	for _, old := range c.recent[k] {
		if old.at.After(cut) {
			keep = append(keep, old)
		}
	}
	c.recent[k] = append(keep, e)
	c.recentMu.Unlock()
	what := answerable(e.kind, e.sub, e.confirmed)
	if what == "" {
		return
	}
	c.share(k, what, sharedRow{ID: r.call.ID, At: e.at})
	if what == answerData {
		return
	}
	if a := c.takeAnswer(k, what); a != nil {
		r.call.Response = a.Response
		c.drop(pair{src: k.dst, dst: k.src}, a.RowID)
	}
}

// pendingAnswer is an answer that found no row to set its response on,
// kept in the KV for the row heard after it, maybe on another replica.
// RowID is the row the answer made of its own, which that row replaces.
type pendingAnswer struct {
	RowID    uint                `json:"row_id"`
	Response models.CallResponse `json:"response"`
}

func answerKey(k pair, what string) string {
	return fmt.Sprintf("%s%d:%d:%s", answerKeyPrefix, k.src, k.dst, what)
}

// keepAnswer keeps an answer of kind what to the row of k as pending.
func (c *CallTracker) keepAnswer(k pair, what string, a pendingAnswer) {
	b, err := json.Marshal(a)
	if err != nil {
		slog.Error("CallTracker: cannot marshal a pending answer", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
	defer cancel()
	key := answerKey(k, what)
	if err := c.kv.Set(ctx, key, b); err != nil {
		slog.Error("CallTracker: cannot keep a pending answer", "error", err)
		return
	}
	if err := c.kv.Expire(ctx, key, responseWindow); err != nil {
		slog.Error("CallTracker: cannot expire a pending answer", "error", err)
	}
}

// takeAnswer returns and removes the pending answer of kind what to the
// row of k, or nil when there is none.
func (c *CallTracker) takeAnswer(k pair, what string) *pendingAnswer {
	ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
	defer cancel()
	key := answerKey(k, what)
	b, err := c.kv.Get(ctx, key)
	if err != nil {
		return nil
	}
	var a pendingAnswer
	if err := json.Unmarshal(b, &a); err != nil {
		return nil
	}
	if err := c.kv.Delete(ctx, key); err != nil {
		slog.Error("CallTracker: cannot delete a pending answer", "error", err)
	}
	return &a
}

// dropRequest asks the replica that holds the row ID, of Src to Dst, to
// delete it.
type dropRequest struct {
	ID  uint `json:"id"`
	Src uint `json:"src"`
	Dst uint `json:"dst"`
}

// drop deletes the row id of k, here or on the replica that holds it.
func (c *CallTracker) drop(k pair, id uint) {
	if c.dropLocal(k, id) {
		return
	}
	msg, err := json.Marshal(dropRequest{ID: id, Src: k.src, Dst: k.dst})
	if err != nil {
		slog.Error("CallTracker: cannot marshal a drop", "error", err)
		return
	}
	if err := c.pubsub.Publish(dropsTopic, msg); err != nil {
		slog.Error("CallTracker: cannot publish a drop", "error", err)
	}
}

func (c *CallTracker) dropLocal(k pair, id uint) bool {
	c.recentMu.Lock()
	var found *recentRow
	for i := range c.recent[k] {
		if c.recent[k][i].row.call.ID == id {
			e := c.recent[k][i]
			found = &e
			break
		}
	}
	c.recentMu.Unlock()
	if found == nil {
		return false
	}
	r := found.row
	c.enqueue(found.shard, task{fn: func(s *shard) { s.drop(r) }})
	return true
}

func (c *CallTracker) remoteDrop(msg []byte) {
	var d dropRequest
	if err := json.Unmarshal(msg, &d); err != nil {
		slog.Error("CallTracker: failed to unmarshal a drop", "error", err)
		return
	}
	c.dropLocal(pair{src: d.Src, dst: d.Dst}, d.ID)
}

// sharedRow is the newest row of a pair of radios that an answer of one
// kind can be set on, in the KV, for the replicas that hear the answer.
type sharedRow struct {
	ID uint      `json:"id"`
	At time.Time `json:"at"`
}

// remoteAnswer asks the replica that holds the row ID, of Src to Dst, to set
// Response on it.
type remoteAnswer struct {
	ID       uint                `json:"id"`
	Src      uint                `json:"src"`
	Dst      uint                `json:"dst"`
	Response models.CallResponse `json:"response"`
}

func recentKey(k pair, what string) string {
	return fmt.Sprintf("%s%d:%d:%s", recentKeyPrefix, k.src, k.dst, what)
}

func (c *CallTracker) share(k pair, what string, sr sharedRow) {
	b, err := json.Marshal(sr)
	if err != nil {
		slog.Error("CallTracker: cannot marshal a shared row", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
	defer cancel()
	key := recentKey(k, what)
	if err := c.kv.Set(ctx, key, b); err != nil {
		slog.Error("CallTracker: cannot share a row", "error", err)
		return
	}
	if err := c.kv.Expire(ctx, key, responseWindow); err != nil {
		slog.Error("CallTracker: cannot expire a shared row", "error", err)
	}
}

// answerable returns the kind of answer that can be set on a row of kind
// and sub, and "" when none can: answerData for a confirmed data packet,
// and the sub kind for a CSBK that a radio answers.
func answerable(kind models.CallKind, sub string, confirmed bool) string {
	switch {
	case (kind == models.CallKindData || kind == models.CallKindUDT) && sub != subResponse && confirmed:
		return answerData
	case kind == models.CallKindCSBK && (sub == subRadioCheck || sub == subCallAlert):
		return sub
	}
	return ""
}

func (c *CallTracker) pruneRecent() {
	c.recentMu.Lock()
	defer c.recentMu.Unlock()
	cut := time.Now().Add(-responseWindow)
	for k, rows := range c.recent {
		if len(rows) == 0 || !rows[len(rows)-1].at.After(cut) {
			delete(c.recent, k)
		}
	}
}

// answer sets resp on the newest row from dst to src within the response
// window that an answer of kind what can be set on, and reports whether
// there is one. The row can be on another replica.
func (c *CallTracker) answer(src, dst uint, resp models.CallResponse, what string) bool {
	k := pair{src: dst, dst: src}
	if c.answerLocal(k, resp, func(r *recentRow) bool { return answerable(r.kind, r.sub, r.confirmed) == what }) {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
	defer cancel()
	b, err := c.kv.Get(ctx, recentKey(k, what))
	if err != nil {
		return false
	}
	var sr sharedRow
	if err := json.Unmarshal(b, &sr); err != nil || time.Since(sr.At) >= responseWindow {
		return false
	}
	msg, err := json.Marshal(remoteAnswer{ID: sr.ID, Src: k.src, Dst: k.dst, Response: resp})
	if err != nil {
		slog.Error("CallTracker: cannot marshal an answer", "error", err)
		return false
	}
	if err := c.pubsub.Publish(responsesTopic, msg); err != nil {
		slog.Error("CallTracker: cannot publish an answer", "error", err)
		return false
	}
	return true
}

func (c *CallTracker) answerLocal(k pair, resp models.CallResponse, match func(*recentRow) bool) bool {
	c.recentMu.Lock()
	rows := c.recent[k]
	cut := time.Now().Add(-responseWindow)
	var found *recentRow
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].at.After(cut) && match(&rows[i]) {
			e := rows[i]
			found = &e
			break
		}
	}
	c.recentMu.Unlock()
	if found == nil {
		return false
	}
	r := found.row
	c.enqueue(found.shard, task{fn: func(s *shard) { s.setResponse(r, resp) }})
	return true
}

func (c *CallTracker) remoteResponse(msg []byte) {
	var a remoteAnswer
	if err := json.Unmarshal(msg, &a); err != nil {
		slog.Error("CallTracker: failed to unmarshal an answer", "error", err)
		return
	}
	c.answerLocal(pair{src: a.Src, dst: a.Dst}, a.Response, func(r *recentRow) bool { return r.row.call.ID == a.ID })
}

func (c *CallTracker) publishCall(call *models.Call) {
	if (call.IsToRepeater || call.IsToTalkgroup) && call.GroupCall {
		rows, err := apimodels.NewWSCallResponsesFor(c.db, nil, models.CurrentSMSWindow(), []models.Call{*call})
		if err != nil || len(rows) != 1 {
			slog.Error("Error showing call", "error", err)
			return
		}
		callJSON, err := json.Marshal(rows[0])
		if err != nil {
			slog.Error("Error marshalling call JSON", "error", err)
			return
		}
		if err = c.pubsub.Publish("calls:public", callJSON); err != nil {
			slog.Error("Error publishing call JSON", "error", err)
			return
		}
	}

	origCallJSON, err := json.Marshal(call)
	if err != nil {
		slog.Error("Error marshalling call JSON", "error", err)
		return
	}
	if err = c.pubsub.Publish("calls", origCallJSON); err != nil {
		slog.Error("Error publishing call JSON", "error", err)
	}
}

// publishNetCheckIn publishes a check-in event if the call's talkgroup has an active net.
func (c *CallTracker) publishNetCheckIn(call *models.Call) {
	if call.Kind != models.CallKindVoice || call.UserID == nil || !call.GroupCall || call.ToTalkgroupID == nil {
		return
	}

	netID, ok := c.activeNets.Load(*call.ToTalkgroupID)
	if !ok {
		return
	}

	checkIn := apimodels.WSNetCheckInResponse{
		NetID:  netID,
		CallID: call.ID,
		User: apimodels.WSCallResponseUser{
			ID:       call.User.ID,
			Callsign: call.User.Callsign,
		},
		StartTime: call.StartTime,
		Duration:  call.Duration,
	}

	data, err := json.Marshal(checkIn)
	if err != nil {
		slog.Error("CallTracker: failed to marshal net check-in", "error", err)
		return
	}

	topic := fmt.Sprintf("net:checkins:%d", netID)
	if err := c.pubsub.Publish(topic, data); err != nil {
		slog.Error("CallTracker: failed to publish net check-in", "error", err)
	}
}

func (c *CallTracker) resolve(call *models.Call, peer bool) {
	if user, err := models.FindUserByID(c.db, call.SourceID); err == nil {
		call.UserID, call.User = &user.ID, user
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Error("CallTracker: error finding source user", "userID", call.SourceID, "error", err)
	}
	if !peer && call.IngressID != 0 {
		if repeater, err := models.FindRepeaterByID(c.db, call.IngressID); err == nil {
			call.RepeaterID, call.Repeater = &repeater.ID, repeater
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("CallTracker: error finding source repeater", "repeaterID", call.IngressID, "error", err)
		}
	}
	dst := call.DestinationID
	if call.GroupCall {
		if tg, err := models.FindTalkgroupByID(c.db, dst); err == nil {
			call.IsToTalkgroup, call.ToTalkgroupID, call.ToTalkgroup = true, &tg.ID, tg
			return
		}
	} else if user, err := models.FindUserByID(c.db, dst); err == nil {
		call.IsToUser, call.ToUserID, call.ToUser = true, &user.ID, user
		return
	}
	if repeater, err := models.FindRepeaterByID(c.db, dst); err == nil {
		call.IsToRepeater, call.ToRepeaterID, call.ToRepeater = true, &repeater.ID, repeater
	}
}

type row struct {
	call       *models.Call
	ch         *channel
	key        any
	dataKey    *dataKey
	last       time.Time
	hang       time.Duration
	finished   bool
	deleted    bool
	delivered  bool
	remembered bool
	ns         uint8
	hasNS      bool
}

type channel struct {
	key  chanKey
	peer bool
	// rxs holds a data receiver for each stream, so that the blocks of two
	// streams on one slot never make one message.
	rxs  map[uint]*data.Receiver
	data map[dataKey]*row
	last time.Time
}

type shard struct {
	t        *CallTracker
	idx      int
	in       chan task
	channels map[chanKey]*channel
	rows     map[any]*row
	streams  map[uint]*streamRows
	aliases  map[sourceKey]*sourceState
	burst    *layer2.Burst
	events   []data.Event
}

func newShard(t *CallTracker, idx int) *shard {
	return &shard{
		t:        t,
		idx:      idx,
		in:       make(chan task, shardDepth),
		channels: map[chanKey]*channel{},
		rows:     map[any]*row{},
		streams:  map[uint]*streamRows{},
		aliases:  map[sourceKey]*sourceState{},
		burst:    layer2.NewBurst(),
		events:   make([]data.Event, 0, data.MaxEvents),
	}
}

func (s *shard) run() {
	defer s.t.wg.Done()
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case t := <-s.in:
			switch {
			case t.stop:
				s.finishAll()
				return
			case t.fn != nil:
				t.fn(s)
			default:
				s.handle(&t)
			}
		case <-ticker.C:
			s.sweep(time.Now())
		}
	}
}

func (s *shard) channel(key chanKey, peer bool) *channel {
	ch, ok := s.channels[key]
	if !ok {
		ch = &channel{key: key, peer: peer, data: map[dataKey]*row{}, rxs: map[uint]*data.Receiver{}}
		s.channels[key] = ch
	}
	return ch
}

func (s *shard) open(t *task, ch *channel, key any, kind models.CallKind, sub string, hang time.Duration) *row {
	p := &t.p
	call := &models.Call{
		StreamID:       p.StreamID,
		StartTime:      t.at,
		Active:         true,
		Kind:           kind,
		SubKind:        sub,
		SourceID:       p.Src,
		Transport:      t.key.server,
		IngressID:      p.Repeater,
		TimeSlot:       p.Slot,
		GroupCall:      p.GroupCall,
		DestinationID:  p.Dst,
		LastPacketTime: t.at,
		LastSeq:        256,
	}
	s.t.resolve(call, t.peer)
	if err := s.t.db.Omit(clause.Associations, "CallData").Create(call).Error; err != nil {
		slog.Error("CallTracker: error creating call", "kind", kind, "error", err)
		return nil
	}
	r := &row{call: call, ch: ch, key: key, last: t.at, hang: hang}
	if key != nil {
		s.rows[key] = r
	}
	s.note(t, r)
	slog.Debug("CallTracker: transmission started", "kind", kind, "sub", sub, "streamID", p.StreamID, "src", p.Src, "dst", p.Dst)
	return r
}

func (s *shard) touch(r *row, t *task) {
	r.last = t.at
	r.call.LastPacketTime = t.at
	if len(r.call.CallData)+len(t.p.DMRData) <= maxCallData {
		r.call.CallData = append(r.call.CallData, t.p.DMRData[:]...)
	}
	s.note(t, r)
}

// streamRows holds the rows that the bursts of one stream went to, each
// from the ID of its first burst there, oldest first.
type streamRows struct {
	from []uint64
	rows []*row
	last uint64
}

func (s *shard) note(t *task, r *row) {
	if t.id == 0 {
		return
	}
	st := s.streams[t.p.StreamID]
	if st == nil {
		st = &streamRows{}
		s.streams[t.p.StreamID] = st
	}
	st.last = t.id
	if n := len(st.rows); n > 0 && st.rows[n-1] == r {
		return
	}
	if len(st.rows) == maxStreamRows {
		st.from, st.rows = append(st.from[:0], st.from[1:]...), append(st.rows[:0], st.rows[1:]...)
	}
	st.from, st.rows = append(st.from, t.id), append(st.rows, r)
}

func (st *streamRows) row(id uint64) *row {
	n := len(st.rows)
	if id == 0 {
		return st.rows[n-1]
	}
	if id > st.last || id < st.from[0] {
		return nil
	}
	for i := n - 1; i >= 0; i-- {
		if st.from[i] <= id {
			return st.rows[i]
		}
	}
	return nil
}

func (s *shard) finish(r *row, at time.Time) {
	if r == nil || r.finished {
		return
	}
	r.finished = true
	if r.key != nil && s.rows[r.key] == r {
		delete(s.rows, r.key)
	}
	if r.ch != nil && r.dataKey != nil && r.ch.data[*r.dataKey] == r {
		delete(r.ch.data, *r.dataKey)
	}
	call := r.call
	call.Active = false
	call.Duration = r.last.Sub(call.StartTime)
	if !at.IsZero() && call.Kind == models.CallKindVoice {
		call.Duration = at.Sub(call.StartTime)
	}
	if call.Kind == models.CallKindVoice && !at.IsZero() && call.Duration < keyUp {
		r.deleted = true
		s.t.db.Unscoped().Delete(call)
		return
	}
	if err := s.t.db.Omit(clause.Associations).Save(call).Error; err != nil {
		slog.Error("CallTracker: error saving call", "error", err)
		return
	}
	s.t.publishCall(call)
	s.t.publishNetCheckIn(call)
	slog.Info("Transmission ended", "kind", call.Kind, "sub", call.SubKind, "streamID", call.StreamID, "src", call.SourceID, "dst", call.DestinationID, "repeater", call.IngressID, "duration", call.Duration, "loss", call.Loss*pct, "ber", call.BER*pct, "rssi", call.RSSI, "jitter", call.Jitter)
}

// idle reports whether no data receiver of ch is in the middle of a
// message.
func (ch *channel) idle() bool {
	for _, rx := range ch.rxs {
		if rx.State() != data.StateIdle {
			return false
		}
	}
	return true
}

func (s *shard) finishAll() {
	for _, ch := range s.channels {
		for _, rx := range ch.rxs {
			s.events = rx.Lost(s.t.now(), s.events[:0])
			s.apply(ch, nil, nil, s.events)
		}
		for _, r := range ch.data {
			s.finish(r, time.Time{})
		}
	}
	for _, r := range s.rows {
		s.finish(r, time.Time{})
	}
}

func (s *shard) sweep(now time.Time) {
	mono := s.t.now()
	for _, r := range s.rows {
		if now.Sub(r.last) >= r.hang {
			if r.call.Kind == models.CallKindVoice {
				slog.Debug("Call timed out", "streamID", r.call.StreamID)
			}
			s.finish(r, time.Time{})
		}
	}
	for key, ch := range s.channels {
		for stream, rx := range ch.rxs {
			if at, ok := rx.Deadline(); ok && mono >= at {
				s.events = rx.Tick(mono, s.events[:0])
				s.apply(ch, nil, nil, s.events)
			}
			if _, waiting := rx.Deadline(); !waiting && rx.State() == data.StateIdle {
				delete(ch.rxs, stream)
			}
		}
		for _, r := range ch.data {
			if now.Sub(r.last) >= dataRowHang && ch.idle() {
				s.finish(r, time.Time{})
			}
		}
		idle := channelIdle
		if ch.peer {
			idle = peerChannelIdle
		}
		if len(ch.data) == 0 && now.Sub(ch.last) >= idle && ch.idle() {
			delete(s.channels, key)
		}
	}
	for id, st := range s.streams {
		if r := st.rows[len(st.rows)-1]; r.finished && now.Sub(r.last) >= streamIdle {
			delete(s.streams, id)
		}
	}
	s.pruneSources(now)
	if s.idx == 0 {
		s.t.pruneRecent()
		s.t.sources.Range(func(k sourceKey, ref streamRef) bool {
			if now.Sub(ref.at) >= streamIdle {
				s.t.sources.Delete(k)
			}
			return true
		})
		s.t.streams.Range(func(id uint, ref streamRef) bool {
			if now.Sub(ref.at) >= streamIdle {
				s.t.streams.Delete(id)
			}
			return true
		})
	}
}

func (s *shard) setResponse(r *row, resp models.CallResponse) {
	if r.deleted {
		return
	}
	r.call.Response = resp
	if !r.finished {
		return
	}
	if err := s.t.db.Model(&models.Call{}).Where("id = ?", r.call.ID).Update("response", resp).Error; err != nil {
		slog.Error("CallTracker: error saving response", "error", err)
		return
	}
	s.t.publishCall(r.call)
}

// drop deletes the row r, which another row replaces.
func (s *shard) drop(r *row) {
	if r.deleted {
		return
	}
	r.deleted = true
	if r.key != nil && s.rows[r.key] == r {
		delete(s.rows, r.key)
	}
	if err := s.t.db.Unscoped().Delete(r.call).Error; err != nil {
		slog.Error("CallTracker: error deleting a replaced call", "error", err)
	}
}

func (s *shard) undeliverable(stream uint, id uint64, target string) {
	st, ok := s.streams[stream]
	if !ok {
		return
	}
	r := st.row(id)
	if r == nil || r.deleted {
		return
	}
	for _, t := range r.call.Undeliverable {
		if t == target {
			return
		}
	}
	r.call.Undeliverable = append(r.call.Undeliverable, target)
	if !r.finished {
		return
	}
	if err := s.t.db.Model(&models.Call{}).Where("id = ?", r.call.ID).Select("undeliverable").Updates(&models.Call{Undeliverable: r.call.Undeliverable}).Error; err != nil {
		slog.Error("CallTracker: error saving undeliverable targets", "error", err)
	}
}
