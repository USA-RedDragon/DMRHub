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
	"cmp"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
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
	matchKeyPrefix  = "dmrhub:calltracker:match:"
	responsesTopic  = "calltracker:responses"
	settleTopic     = "calltracker:settle"
	// matchLag is how long a worker may lag behind the newest row or
	// answer of a pair and still find what it needs in the match.
	matchLag = time.Minute
	// matchCap bounds the rows and the answers that a match keeps, as a
	// backstop to the pruning by time.
	matchCap    = 512
	settleTries = 16
	dropsTopic  = "calltracker:drops"

	undeliverableTopic = "calltracker:undeliverable"
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
	clock  clock.Clock
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

	streams *xsync.Map[uint, streamRef]
	sources *xsync.Map[sourceKey, streamRef]
	leads   *xsync.Map[leadKey, lead]
	joined  *xsync.Map[chanKey, lead]
	// held holds the rows of this replica that answers can be set on, and
	// owned the rows that answers made of their own, while their workers
	// have them, by ID.
	held  *xsync.Map[uint, heldRow]
	owned *xsync.Map[uint, heldRow]
	// homes holds, for the match window and lag after their finish, the
	// workers that saved the rows that answers can be set on here, so that
	// each later event of a row goes out through the outbox of its finish.
	homes *xsync.Map[uint, homeRow]

	observer atomic.Pointer[observerBox]
	plugins  atomic.Pointer[pluginsBox]
	// out publishes what the workers and the callers, which deliver
	// bursts, send to pubsub, so that they never wait on it.
	out     *outbox.Outbox[outKey]
	dropped atomic.Uint64
	metrics *metrics.Metrics
	bursts  atomic.Uint64
	replica string
	deltas  atomic.Uint64
	kvDown  atomic.Int64
	kvFails atomic.Int64
	keyDown *xsync.Map[voiceKey, time.Duration]
}

type observerBox struct{ o DataObserver }

// PluginOwners tells which plugin owns a DMR ID.
type PluginOwners interface {
	Owner(id uint) (uint, bool)
}

type pluginsBox struct{ p PluginOwners }

type streamRef struct {
	shard int
	at    time.Time
}

type pair struct{ src, dst uint }

type heldRow struct {
	shard int
	row   *row
}

type homeRow struct {
	shard int
	until time.Time
}

// Option sets up a CallTracker in NewCallTracker.
type Option func(*CallTracker)

// WithClock makes the call tracker wait and measure on c.
func WithClock(c clock.Clock) Option {
	return func(ct *CallTracker) { ct.clock = clock.Or(c) }
}

// NewCallTracker creates a new CallTracker and starts its workers. The
// replicas of the hub share kv and pubsub, so that an answer heard on one
// replica is set on the row of the transmission that another one heard.
func NewCallTracker(db *gorm.DB, pubsub pubsub.PubSub, kv kv.KV, opts ...Option) *CallTracker {
	ct := &CallTracker{
		clock:      clock.Real(),
		db:         db,
		pubsub:     pubsub,
		kv:         kv,
		activeNets: xsync.NewMap[uint, uint](),
		streams:    xsync.NewMap[uint, streamRef](),
		sources:    xsync.NewMap[sourceKey, streamRef](),
		leads:      xsync.NewMap[leadKey, lead](),
		joined:     xsync.NewMap[chanKey, lead](),
		held:       xsync.NewMap[uint, heldRow](),
		owned:      xsync.NewMap[uint, heldRow](),
		keyDown:    xsync.NewMap[voiceKey, time.Duration](),
		homes:      xsync.NewMap[uint, homeRow](),
		done:       make(chan struct{}),
		metrics:    metrics.NewMetrics(),
	}
	for _, opt := range opts {
		opt(ct)
	}
	ct.epoch = ct.clock.Now()
	ct.out = outbox.New(pubsub, ct.done, "the messages of the call tracker", func(k outKey) []any {
		return []any{"worker", k.shard, "topic", k.topic}
	})
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	ct.bursts.Store(binary.LittleEndian.Uint64(seed[:]) >> (shardBits + 2))
	ct.replica = rand.Text()
	ct.loadActiveNets()
	ct.subscribe("net:events", ct.netEvent)
	ct.subscribe(responsesTopic, ct.remoteResponse)
	ct.subscribe(settleTopic, ct.remoteSettle)
	ct.subscribe(dropsTopic, ct.remoteDrop)
	ct.subscribe(convertedTopic, ct.remoteConverted)
	ct.subscribe(undeliverableTopic, ct.remoteUndeliverable)
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

// SetPlugins makes p the plugins whose IDs the calls go to.
func (c *CallTracker) SetPlugins(p PluginOwners) {
	c.plugins.Store(&pluginsBox{p: p})
}

func (c *CallTracker) toPlugin(id uint) bool {
	b := c.plugins.Load()
	if b == nil || b.p == nil {
		return false
	}
	_, ok := b.p.Owner(id)
	return ok
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

// Track queues the packet p, which the server named server got now, to be
// recorded. peer reports a server of network peers, such as OpenBridge,
// whose Repeater field is a peer ID and which carries many streams on one
// slot. Track never blocks: when the queue of the worker is full, the
// packet is not recorded and Dropped counts it. It returns the ID of the
// burst, for p.TrackID, or zero when it is stopped.
func (c *CallTracker) Track(ctx context.Context, p models.Packet, server string, peer bool) uint64 {
	return c.TrackAt(ctx, p, server, peer, c.clock.Now())
}

// TrackAt is Track for a packet that the server got at at, such as the
// time the kernel got it. The tracker times the transmission from at, so
// a packet that waited in the socket, or that the hub held while it
// checked the claim of the stream, keeps its place in the call. A time
// after now counts as now.
func (c *CallTracker) TrackAt(_ context.Context, p models.Packet, server string, peer bool, at time.Time) uint64 {
	if c.stopped.Load() {
		return 0
	}
	now := c.clock.Now()
	if d := now.Sub(at); d > 0 {
		now = now.Add(-d)
	}
	key := chanKey{server: server, repeater: p.Repeater, slot: p.Slot}
	if peer {
		key.stream = c.peerStream(key, &p, now)
	}
	idx := key.shard()
	id := c.bursts.Add(1)<<shardBits | uint64(idx) //nolint:gosec
	c.streams.Store(p.StreamID, streamRef{shard: idx, at: now})
	if !peer {
		c.sources.Store(sourceKey{server: server, repeater: p.Repeater, src: p.Src}, streamRef{shard: idx, at: now})
	}
	c.enqueue(idx, task{key: key, p: p, peer: peer, at: now, id: id})
	return id
}

// leadKey names the calls of one source to one target on a slot of a
// peer.
type leadKey struct {
	server   string
	repeater uint
	slot     bool
	src, dst uint
	group    bool
}

// lead is the stream whose channel takes the bursts of a call, and when
// the call last had a burst there.
type lead struct {
	stream uint
	at     time.Time
}

// peerStream returns the stream whose channel takes p, a packet of a peer
// on the slot of key, at now. MMDVMHost sends each CSBK and data header
// with a new stream ID, and peers forward them so. A CSBK or data header
// less than preambleGap after a CSBK of the same source, target and group
// flag, and the rest of its stream, go to the channel of that CSBK, as all
// the streams of a repeater slot do, so that the preambles and the data
// that they announce can make one row.
func (c *CallTracker) peerStream(key chanKey, p *models.Packet, now time.Time) uint {
	key.stream = p.StreamID
	if j, ok := c.joined.Load(key); ok {
		c.joined.Store(key, lead{stream: j.stream, at: now})
		return j.stream
	}
	if p.FrameType != dmrconst.FrameDataSync {
		return p.StreamID
	}
	dt := enums.DataType(p.DTypeOrVSeq & 0xf)
	if dt != enums.DataTypeCSBK && dt != enums.DataTypeDataHeader {
		return p.StreamID
	}
	lk := leadKey{server: key.server, repeater: key.repeater, slot: key.slot, src: p.Src, dst: p.Dst, group: p.GroupCall}
	stream := p.StreamID
	if l, ok := c.leads.Load(lk); ok && l.stream != p.StreamID && now.Sub(l.at) < preambleGap {
		stream = l.stream
		c.joined.Store(key, lead{stream: stream, at: now})
	}
	if dt == enums.DataTypeCSBK {
		c.leads.Store(lk, lead{stream: stream, at: now})
	}
	return stream
}

// Undeliverable records why, such as "IPSC repeater 311304200 cannot
// carry this kind of burst.", a target could not take the burst of p, a packet that Track got, on the transmission that
// holds the burst. Call it after Track for the same packet. p.TrackID
// names the burst; without it the mark goes on the latest transmission of
// the stream. The hub counts the burst in its metrics. Any replica may
// call it: a call that another replica tracks gets the mark over pubsub.
func (c *CallTracker) Undeliverable(p models.Packet, target string) {
	n := undeliverableNote{Stream: p.StreamID, Track: p.TrackID, Target: target}
	if c.markUndeliverable(n) || c.pubsub == nil || c.stopped.Load() {
		return
	}
	msg, err := json.Marshal(n)
	if err != nil {
		return
	}
	c.publishLater(undeliverableTopic, msg)
}

// outKey names a queue of the outbox: the messages that the worker shard
// publishes on topic, or, with shard -1, the marks for other replicas.
type outKey struct {
	shard int
	topic string
}

// publishLater queues msg, a mark for other replicas, for the outbox.
func (c *CallTracker) publishLater(topic string, msg []byte) {
	c.publish(-1, topic, msg)
}

// publish queues msg on topic for the outbox, which publishes the messages
// of each worker and topic in order and never makes the worker wait.
func (c *CallTracker) publish(shard int, topic string, msg []byte) {
	if c.pubsub == nil {
		return
	}
	c.out.Add(outKey{shard: shard, topic: topic}, outbox.Job{Topic: topic, Data: msg})
}

type undeliverableNote struct {
	Stream uint   `json:"stream"`
	Track  uint64 `json:"track"`
	Target string `json:"target"`
}

func (c *CallTracker) remoteUndeliverable(msg []byte) {
	var n undeliverableNote
	if err := json.Unmarshal(msg, &n); err != nil || n.Target == "" {
		return
	}
	c.markUndeliverable(n)
}

// markUndeliverable queues n on the worker of its call and reports whether
// the call is tracked here.
func (c *CallTracker) markUndeliverable(n undeliverableNote) bool {
	if c.stopped.Load() {
		return false
	}
	ref, ok := c.streams.Load(n.Stream)
	if !ok {
		return false
	}
	idx := ref.shard
	if n.Track != 0 {
		idx = int(n.Track % shardCount)
	}
	c.enqueue(idx, task{fn: func(s *shard) { s.undeliverable(n.Stream, n.Track, n.Target) }})
	return true
}

// Flush waits until the workers handled every packet queued before it.
func (c *CallTracker) Flush() {
	barriers := make([]chan struct{}, len(c.shards))
	for i := range c.shards {
		barriers[i] = make(chan struct{})
		b := barriers[i]
		select {
		case c.shards[i].in <- task{at: c.clock.Now(), fn: func(*shard) { close(b) }}:
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
	if t.at.IsZero() {
		t.at = c.clock.Now()
	}
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
	return c.clock.Since(c.epoch)
}

func (c *CallTracker) mono(at time.Time) time.Duration {
	return at.Sub(c.epoch)
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

// ActiveNet returns the ID of the net that is active on the talkgroup tg,
// as loaded from the database at start and updated from net:events since,
// and reports whether one is active.
func (c *CallTracker) ActiveNet(tg uint) (uint, bool) {
	return c.activeNets.Load(tg)
}

func (c *CallTracker) remember(s *shard, r *row) {
	if r.call.GroupCall {
		return
	}
	what := answerable(r.call.Kind, r.call.SubKind, r.call.Confirmed)
	if what == "" {
		return
	}
	r.what, r.pair = what, pair{src: r.call.SourceID, dst: r.call.DestinationID}
	c.held.Store(r.call.ID, heldRow{shard: s.idx, row: r})
	res, err := c.step(r.pair, what, matchOp{row: &matchRow{ID: r.call.ID, Start: r.call.StartTime}})
	if err != nil {
		slog.Error("CallTracker: cannot match a row with the answers to it", "error", err)
		c.metrics.IncrementTrackerMatchFailed()
		return
	}
	if resp, ok := res.after[r.call.ID]; ok {
		r.call.Response = resp
	}
	c.applyStep(s.idx, r.pair, what, res, r.call.ID)
}

// heardFirst reports whether an answer of kind what can go to a request
// that the hub hears after it: a radio sends a CSBK answer only to a
// request, so the hub got the request late, while a data response can
// also answer an earlier message.
func heardFirst(what string) bool {
	return what != answerData
}

// inWindow reports whether a row that started at start can take an answer
// that the hub got at at: it started before the answer, within the
// response window.
func inWindow(start, at time.Time) bool {
	return !start.After(at) && start.After(at.Add(-responseWindow))
}

// matchRow is a row that answers can be set on, when it started, and the
// response that the last step gave it, from the answer of RespStream that
// the hub got at RespAt.
type matchRow struct {
	ID         uint                `json:"id"`
	Start      time.Time           `json:"start"`
	Resp       models.CallResponse `json:"resp,omitempty"`
	RespStream uint                `json:"resp_stream,omitempty"`
	RespAt     time.Time           `json:"resp_at,omitzero"`
}

// matchAnswer is an answer of one stream, the time the hub got its newest
// burst and its response. OwnRow is the row the answer made of its own
// while no row takes it, which the row that takes it replaces.
type matchAnswer struct {
	Stream   uint                `json:"stream"`
	At       time.Time           `json:"at"`
	Response models.CallResponse `json:"response"`
	OwnRow   uint                `json:"own_row,omitempty"`
}

// matchState is everything a pair of radios sent that answers of one kind
// can be set on, and their answers, in one KV entry that Update changes in
// one step.
type matchState struct {
	Rows    []matchRow    `json:"rows,omitempty"`
	Answers []matchAnswer `json:"answers,omitempty"`
}

// assignment is what a matchState makes of its answers: the row that each
// answer stream goes to, and the response of each row and the answer it
// comes from.
type assignment struct {
	winner map[uint]uint
	resp   map[uint]models.CallResponse
	from   map[uint]matchAnswer
}

// assign gives each answer to the row that started last within its
// window, the larger ID on a tie, and each row the response of the newest
// answer it takes, or none. With heardFirst, an answer that no row before
// it takes goes to the row that started first within the window after
// it, the smaller ID on a tie, as a CSBK request that the hub hears after
// its answer. See keepDropped for the answers that the match dropped.
func (st *matchState) assign(heardFirst bool) assignment {
	a := assignment{winner: map[uint]uint{}, resp: map[uint]models.CallResponse{}, from: map[uint]matchAnswer{}}
	rows := slices.Clone(st.Rows)
	slices.SortFunc(rows, func(x, y matchRow) int {
		if c := x.Start.Compare(y.Start); c != 0 {
			return c
		}
		return cmp.Compare(x.ID, y.ID)
	})
	for _, r := range rows {
		a.resp[r.ID] = models.CallResponseNone
	}
	for _, ans := range st.Answers {
		after, _ := slices.BinarySearchFunc(rows, ans.At, func(r matchRow, t time.Time) int {
			if r.Start.After(t) {
				return 1
			}
			return -1
		})
		var best *matchRow
		if i := after - 1; i >= 0 && rows[i].Start.After(ans.At.Add(-responseWindow)) {
			best = &rows[i]
		} else if heardFirst && after < len(rows) && rows[after].Start.Before(ans.At.Add(responseWindow)) {
			best = &rows[after]
		}
		if best == nil {
			continue
		}
		a.winner[ans.Stream] = best.ID
		if f, ok := a.from[best.ID]; !ok || ans.At.After(f.At) {
			a.from[best.ID], a.resp[best.ID] = ans, ans.Response
		}
	}
	st.keepDropped(&a)
	return a
}

// keepDropped gives each row whose response came from an answer that the
// match dropped that response again, unless an answer that the match
// still has, and that the hub got later, now gives the row its response.
// So pruning and the cap never take a response away, and a response from
// an answer that the match still has follows that answer.
func (st *matchState) keepDropped(a *assignment) {
	held := make(map[uint]bool, len(st.Answers))
	for _, ans := range st.Answers {
		held[ans.Stream] = true
	}
	for _, r := range st.Rows {
		if r.RespStream == 0 || held[r.RespStream] {
			continue
		}
		if f, ok := a.from[r.ID]; ok && !r.RespAt.After(f.At) {
			continue
		}
		a.resp[r.ID] = r.Resp
		a.from[r.ID] = matchAnswer{Stream: r.RespStream, At: r.RespAt, Response: r.Resp}
	}
}

func (st *matchState) newest() time.Time {
	var t time.Time
	for _, r := range st.Rows {
		if r.Start.After(t) {
			t = r.Start
		}
	}
	for _, a := range st.Answers {
		if a.At.After(t) {
			t = a.At
		}
	}
	return t
}

// prune drops the answers more than the window and matchLag older than
// ref, and the rows that none of the answers it keeps can take.
func (st *matchState) prune(ref time.Time) {
	answerCut := ref.Add(-responseWindow - matchLag)
	rowCut := answerCut.Add(-responseWindow)
	answers := st.Answers[:0]
	for _, a := range st.Answers {
		if a.At.After(answerCut) {
			answers = append(answers, a)
		}
	}
	st.Answers = answers
	rows := st.Rows[:0]
	for _, r := range st.Rows {
		if r.Start.After(rowCut) {
			rows = append(rows, r)
		}
	}
	st.Rows = rows
}

// limit keeps the newest matchCap answers and rows.
func (st *matchState) limit() {
	if n := len(st.Answers); n > matchCap {
		slices.SortFunc(st.Answers, func(a, b matchAnswer) int { return a.At.Compare(b.At) })
		st.Answers = st.Answers[n-matchCap:]
	}
	if n := len(st.Rows); n > matchCap {
		slices.SortFunc(st.Rows, func(a, b matchRow) int { return a.Start.Compare(b.Start) })
		st.Rows = st.Rows[n-matchCap:]
	}
}

// matchOp is the row or the answer that a step adds. keep keeps an answer
// that no row takes.
type matchOp struct {
	row    *matchRow
	answer *matchAnswer
	keep   bool
}

func (op matchOp) at() time.Time {
	if op.row != nil {
		return op.row.Start
	}
	return op.answer.At
}

// stepResult is what a step changed: the response of each row whose
// response it changed, the response of every row after it, the row that
// the answer it added goes to, and the rows that answers made of their own
// that a row now replaces.
type stepResult struct {
	changed map[uint]models.CallResponse
	after   map[uint]models.CallResponse
	winner  uint
	drops   []uint
}

var errNothingToKeep = errors.New("an answer that no row takes and that is not kept")

func (st *matchState) add(op matchOp) {
	if op.row != nil {
		if !slices.ContainsFunc(st.Rows, func(r matchRow) bool { return r.ID == op.row.ID }) {
			st.Rows = append(st.Rows, *op.row)
		}
		return
	}
	a := *op.answer
	for i := range st.Answers {
		old := &st.Answers[i]
		if old.Stream != a.Stream {
			continue
		}
		if !a.At.Before(old.At) {
			old.At, old.Response = a.At, a.Response
		}
		if a.OwnRow != 0 {
			old.OwnRow = a.OwnRow
		}
		return
	}
	st.Answers = append(st.Answers, a)
}

func matchKey(k pair, what string) string {
	return fmt.Sprintf("%s%d:%d:%s", matchKeyPrefix, k.src, k.dst, what)
}

// step adds the row or the answer of op to the match of k and what and
// matches the answers again, in one Update of the KV.
func (c *CallTracker) step(k pair, what string, op matchOp) (stepResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
	defer cancel()
	var res stepResult
	err := c.kv.Update(ctx, matchKey(k, what), responseWindow+matchLag, func(old []byte) ([]byte, error) {
		var st matchState
		if old != nil && json.Unmarshal(old, &st) != nil {
			st = matchState{}
		}
		ref := st.newest()
		if op.at().After(ref) {
			ref = op.at()
		}
		st.prune(ref)
		before := st.assign(heardFirst(what))
		st.add(op)
		after := st.assign(heardFirst(what))
		res = stepResult{changed: map[uint]models.CallResponse{}, after: after.resp}
		if op.answer != nil {
			res.winner = after.winner[op.answer.Stream]
			if res.winner == 0 && !op.keep {
				return nil, errNothingToKeep
			}
		}
		for id, resp := range after.resp {
			if before.resp[id] != resp {
				res.changed[id] = resp
			}
		}
		for i := range st.Answers {
			a := &st.Answers[i]
			if a.OwnRow == 0 || after.winner[a.Stream] == 0 {
				continue
			}
			if op.answer == nil || a.OwnRow != op.answer.OwnRow {
				res.drops = append(res.drops, a.OwnRow)
			}
			a.OwnRow = 0
		}
		for i := range st.Rows {
			r := &st.Rows[i]
			f := after.from[r.ID]
			r.Resp, r.RespStream, r.RespAt = after.resp[r.ID], f.Stream, f.At
		}
		st.limit()
		return json.Marshal(st)
	})
	if errors.Is(err, errNothingToKeep) {
		return stepResult{}, nil
	}
	if err != nil {
		return stepResult{}, fmt.Errorf("matching the answers of %d to %d: %w", k.src, k.dst, err)
	}
	return res, nil
}

// applyStep sets the responses that a step changed on their rows, except
// on self, which the caller sets, and deletes the rows that answers made
// of their own that rows now replace. The worker idx calls it.
func (c *CallTracker) applyStep(idx int, k pair, what string, res stepResult, self uint) {
	for id := range res.changed {
		if id != self {
			c.settle(idx, k, what, id)
		}
	}
	for _, id := range res.drops {
		c.dropOwn(idx, k, id)
	}
}

// matched returns the response that the match of k and what gives the row
// id, and false when the match or the row is no longer in the KV, so that
// the row stays as it is.
func (c *CallTracker) matched(k pair, what string, id uint) (models.CallResponse, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
	defer cancel()
	b, err := c.kv.Get(ctx, matchKey(k, what))
	if err != nil {
		return models.CallResponseNone, false
	}
	var st matchState
	if json.Unmarshal(b, &st) != nil {
		return models.CallResponseNone, false
	}
	resp, ok := st.assign(heardFirst(what)).resp[id]
	return resp, ok
}

// settleRequest asks the replica whose worker holds the row ID, of Src to
// Dst, to set its response again from the match of kind What.
type settleRequest struct {
	ID   uint   `json:"id"`
	Src  uint   `json:"src"`
	Dst  uint   `json:"dst"`
	What string `json:"what"`
}

// settle sets the response of the row id of k from the match of kind
// what: on its worker when a worker here holds it or saved it, and
// otherwise in the database, while it asks the replica whose worker holds
// or saved it to do the same. The worker idx calls it.
func (c *CallTracker) settle(idx int, k pair, what string, id uint) {
	if c.settleHere(k, what, id) {
		return
	}
	if msg, err := json.Marshal(settleRequest{ID: id, Src: k.src, Dst: k.dst, What: what}); err == nil {
		c.publish(idx, settleTopic, msg)
	}
	c.settleSaved(idx, k, what, id)
}

func (s *shard) settleHeld(r *row) {
	if r.deleted {
		return
	}
	if r.finished {
		s.t.settleSaved(s.idx, r.pair, r.what, r.call.ID)
		return
	}
	if resp, ok := s.t.matched(r.pair, r.what, r.call.ID); ok {
		r.call.Response = resp
	}
}

// settleSaved sets the response of the saved row id from the match and
// publishes the row, and does it again while the match changes the
// response under it, at most settleTries times. Each step that changes the
// response of a row settles it after the step, so the last write reflects
// the last step.
func (c *CallTracker) settleSaved(idx int, k pair, what string, id uint) {
	for range settleTries {
		resp, ok := c.matched(k, what, id)
		if !ok {
			return
		}
		if err := c.db.Model(&models.Call{}).Where("id = ?", id).Update("response", resp).Error; err != nil {
			slog.Error("CallTracker: error saving response", "error", err)
			return
		}
		var call models.Call
		if err := c.db.Preload(clause.Associations).First(&call, id).Error; err == nil {
			c.publishCall(idx, &call)
		}
		if again, ok := c.matched(k, what, id); !ok || again == resp {
			return
		}
	}
	slog.Warn("CallTracker: the response of a row kept changing while it was saved", "id", id)
}

func (c *CallTracker) remoteSettle(msg []byte) {
	var q settleRequest
	if err := json.Unmarshal(msg, &q); err != nil {
		slog.Error("CallTracker: failed to unmarshal a settle", "error", err)
		return
	}
	c.settleHere(pair{src: q.Src, dst: q.Dst}, q.What, q.ID)
}

// settleHere queues the settle of the row id on the worker here that
// holds it or saved it, and reports whether there is one.
func (c *CallTracker) settleHere(k pair, what string, id uint) bool {
	if h, ok := c.held.Load(id); ok {
		c.enqueue(h.shard, task{fn: func(s *shard) { s.settleHeld(h.row) }})
		return true
	}
	if h, ok := c.homes.Load(id); ok {
		c.enqueue(h.shard, task{fn: func(s *shard) { s.t.settleSaved(s.idx, k, what, id) }})
		return true
	}
	return false
}

// dropRequest asks the replica whose worker holds the row ID, of Src to
// Dst, to delete it.
type dropRequest struct {
	ID  uint `json:"id"`
	Src uint `json:"src"`
	Dst uint `json:"dst"`
}

// dropOwn deletes the row id that an answer to the rows of k made of its
// own: on its worker when a worker here holds it, and otherwise in the
// database, while it asks the replica whose worker holds it to do the
// same. The worker idx calls it.
func (c *CallTracker) dropOwn(idx int, k pair, id uint) {
	if h, ok := c.owned.Load(id); ok {
		c.enqueue(h.shard, task{fn: func(s *shard) { s.drop(h.row) }})
		return
	}
	if msg, err := json.Marshal(dropRequest{ID: id, Src: k.dst, Dst: k.src}); err == nil {
		c.publish(idx, dropsTopic, msg)
	}
	if err := c.db.Unscoped().Delete(&models.Call{ID: id}).Error; err != nil {
		slog.Error("CallTracker: error deleting a replaced call", "error", err)
	}
}

func (c *CallTracker) remoteDrop(msg []byte) {
	var d dropRequest
	if err := json.Unmarshal(msg, &d); err != nil {
		slog.Error("CallTracker: failed to unmarshal a drop", "error", err)
		return
	}
	if h, ok := c.owned.Load(d.ID); ok {
		c.enqueue(h.shard, task{fn: func(s *shard) { s.drop(h.row) }})
	}
}

// remoteAnswer asks the replica that holds the row ID, of Src to Dst, to
// set Response on it. Replicas before the match in the KV send it.
type remoteAnswer struct {
	ID       uint                `json:"id"`
	Src      uint                `json:"src"`
	Dst      uint                `json:"dst"`
	Response models.CallResponse `json:"response"`
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

// answer adds resp, an answer of stream that the hub got at at, from src
// to dst, to the match of the rows from dst to src that answers of kind
// what can be set on, and reports whether a row takes it: the row that
// started last before at, within the response window, here or on another
// replica, now or once it is made. When no row takes it and keep is set,
// the answer stays in the match with own, the row it made of its own.
// The worker idx calls it.
func (c *CallTracker) answer(idx int, src, dst, stream uint, resp models.CallResponse, what string, at time.Time, own uint, keep bool) bool {
	k := pair{src: dst, dst: src}
	res, err := c.step(k, what, matchOp{answer: &matchAnswer{Stream: stream, At: at, Response: resp, OwnRow: own}, keep: keep})
	if err != nil {
		slog.Error("CallTracker: cannot match an answer with its row", "error", err)
		c.metrics.IncrementTrackerMatchFailed()
		return false
	}
	c.applyStep(idx, k, what, res, 0)
	return res.winner != 0
}

func (c *CallTracker) remoteResponse(msg []byte) {
	var a remoteAnswer
	if err := json.Unmarshal(msg, &a); err != nil {
		slog.Error("CallTracker: failed to unmarshal an answer", "error", err)
		return
	}
	if h, ok := c.held.Load(a.ID); ok {
		c.enqueue(h.shard, task{fn: func(s *shard) { s.setResponse(h.row, a.Response) }})
	}
}

// publishCall publishes call for the web clients. The worker idx calls it.
func (c *CallTracker) publishCall(idx int, call *models.Call) {
	if (call.IsToRepeater || call.IsToTalkgroup || call.IsToPlugin) && call.GroupCall {
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
		c.publish(idx, "calls:public", callJSON)
	}

	origCallJSON, err := json.Marshal(call)
	if err != nil {
		slog.Error("Error marshalling call JSON", "error", err)
		return
	}
	c.publish(idx, "calls", origCallJSON)
}

// publishNetCheckIn publishes a check-in event if the call's talkgroup has
// an active net. The worker idx calls it.
func (c *CallTracker) publishNetCheckIn(idx int, call *models.Call) {
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
	c.publish(idx, topic, data)
}

func (c *CallTracker) resolve(call *models.Call, peer bool) {
	if user, err := models.FindUserWithoutRepeaters(c.db, call.SourceID); err == nil {
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
	} else if user, err := models.FindUserWithoutRepeaters(c.db, dst); err == nil {
		call.IsToUser, call.ToUserID, call.ToUser = true, &user.ID, user
		return
	}
	if c.toPlugin(dst) {
		call.IsToPlugin = true
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
	hung       bool
	deleted    bool
	delivered  bool
	remembered bool
	// what is the kind of answer that can be set on the row, of the radios
	// of pair, or "".
	what      string
	pair      pair
	ns        uint8
	hasNS     bool
	converted map[string]bool
	shared    bool
	deferred  bool
	given     given
	touched   time.Time
	pending   []voiceDelta
	waitSince time.Time
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
	ended    map[voiceKey]*row
	waiting  map[*row]time.Time
	streams  map[uint]*streamRows
	aliases  map[sourceKey]*sourceState
	burst    *layer2.Burst
	events   []data.Event
	clock    time.Time
}

func newShard(t *CallTracker, idx int) *shard {
	return &shard{
		t:        t,
		idx:      idx,
		in:       make(chan task, shardDepth),
		channels: map[chanKey]*channel{},
		rows:     map[any]*row{},
		ended:    map[voiceKey]*row{},
		waiting:  map[*row]time.Time{},
		streams:  map[uint]*streamRows{},
		aliases:  map[sourceKey]*sourceState{},
		burst:    layer2.NewBurst(),
		events:   make([]data.Event, 0, data.MaxEvents),
	}
}

func (s *shard) run() {
	defer s.t.wg.Done()
	ticker := s.t.clock.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case t := <-s.in:
			if t.at.After(s.clock) {
				s.clock = t.at
			}
			switch {
			case t.stop:
				s.finishAll()
				return
			case t.fn != nil:
				t.fn(s)
			default:
				s.handle(&t)
			}
		case <-ticker.C():
			now := s.t.clock.Now()
			if len(s.in) > 0 {
				now = s.clock
			}
			s.sweep(now)
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
	call := s.newCall(t, kind, sub)
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
	slog.Debug("CallTracker: transmission started", "kind", kind, "sub", sub, "streamID", t.p.StreamID, "src", t.p.Src, "dst", t.p.Dst)
	return r
}

func (s *shard) newCall(t *task, kind models.CallKind, sub string) *models.Call {
	p := &t.p
	return &models.Call{
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
	r.hung = at.IsZero()
	if r.key != nil && s.rows[r.key] == r {
		delete(s.rows, r.key)
	}
	if k, ok := r.key.(voiceKey); ok {
		s.ended[k] = r
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
	if r.shared {
		s.finishShared(r, at)
		return
	}
	if call.Kind == models.CallKindVoice && !at.IsZero() && call.Duration < keyUp {
		r.deleted = true
		s.t.db.Unscoped().Delete(call)
		return
	}
	if r.what != "" {
		if resp, ok := s.t.matched(r.pair, r.what, call.ID); ok {
			call.Response = resp
		}
	}
	err := s.t.db.Omit(clause.Associations).Save(call).Error
	s.t.owned.Delete(call.ID)
	if r.what != "" {
		s.t.homes.Store(call.ID, homeRow{shard: s.idx, until: s.t.clock.Now().Add(responseWindow + matchLag)})
		s.t.held.Delete(call.ID)
	}
	if err != nil {
		slog.Error("CallTracker: error saving call", "error", err)
		return
	}
	s.t.publishCall(s.idx, call)
	if r.what != "" {
		if resp, ok := s.t.matched(r.pair, r.what, call.ID); ok && resp != call.Response {
			s.t.settleSaved(s.idx, r.pair, r.what, call.ID)
		}
	}
	s.t.publishNetCheckIn(s.idx, call)
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
		for stream, rx := range ch.rxs {
			s.events = rx.Lost(s.t.now(), s.events[:0])
			s.apply(ch, stream, nil, nil, s.events)
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
	mono := s.t.mono(now)
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
				s.apply(ch, stream, nil, nil, s.events)
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
	for k, r := range s.ended {
		if now.Sub(r.last) >= streamIdle {
			delete(s.ended, k)
		}
	}
	for id, st := range s.streams {
		if r := st.rows[len(st.rows)-1]; r.finished && now.Sub(r.last) >= streamIdle {
			delete(s.streams, id)
		}
	}
	s.touchShared(now)
	s.retryWaiting(now)
	s.pruneSources(now)
	if s.idx == 0 {
		s.t.homes.Range(func(id uint, h homeRow) bool {
			if h.until.Before(now) {
				s.t.homes.Delete(id)
			}
			return true
		})
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
		s.t.leads.Range(func(k leadKey, l lead) bool {
			if now.Sub(l.at) >= streamIdle {
				s.t.leads.Delete(k)
			}
			return true
		})
		s.t.joined.Range(func(k chanKey, l lead) bool {
			if now.Sub(l.at) >= streamIdle {
				s.t.joined.Delete(k)
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
	s.t.publishCall(s.idx, r.call)
}

// drop deletes the row r, which another row replaces.
func (s *shard) drop(r *row) {
	if r.deleted {
		return
	}
	r.deleted = true
	s.t.owned.Delete(r.call.ID)
	s.t.held.Delete(r.call.ID)
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
	if r.shared {
		s.notePart(r)
		return
	}
	if err := s.t.db.Model(&models.Call{}).Where("id = ?", r.call.ID).Select("undeliverable").Updates(&models.Call{Undeliverable: r.call.Undeliverable}).Error; err != nil {
		slog.Error("CallTracker: error saving undeliverable targets", "error", err)
	}
}
