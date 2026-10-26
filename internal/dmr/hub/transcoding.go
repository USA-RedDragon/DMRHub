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

package hub

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
)

const (
	// needTTL is how long the hub keeps its answer to whether a stream
	// has a target in the other codec, so that a target that joins in the
	// middle of a call gets the copy.
	needTTL = time.Second
	// tgRepeatersTTL is how long the hub keeps the repeaters of a
	// talkgroup for that answer.
	tgRepeatersTTL = 2 * time.Second
	// transcodeQueue is the fewest bursts of one stream that may wait for
	// the transcoder; queueLen holds as many as arrive within the budget
	// of a burst. A burst over it gets no copy, and its audio counts as
	// missed.
	transcodeQueue = 8
	burstPeriod    = 60 * time.Millisecond
)

// queueLen returns the bursts that may wait for a transcoder whose bursts
// may each take budget: those that arrive while one is converted, and at
// least transcodeQueue.
func queueLen(budget time.Duration) int {
	return max(transcodeQueue, int(budget/burstPeriod)+1)
}

func (sc *streamCodec) transcoder() *transcoder {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.tx
}

// setTranscoder makes tx the transcoder of the stream, and stops the one
// before.
func (sc *streamCodec) setTranscoder(tx *transcoder) {
	sc.mu.Lock()
	old := sc.tx
	sc.tx = tx
	sc.mu.Unlock()
	if old != nil && old != tx {
		old.stop()
	}
}

type copyReport struct {
	stream uint
	target string
}

// SetAMBEServers sets the AMBE servers that convert calls between Codec2
// and AMBE, in the order a call tries them. Each call holds one server for
// its whole length. An empty list turns transcoding off; calls then pass
// through in their own codec.
func (h *Hub) SetAMBEServers(list []transcode.ServerConfig) {
	h.transcoder.SetServers(list)
}

// Transcoder returns the service that converts calls between codecs.
func (h *Hub) Transcoder() *transcode.Service {
	return h.transcoder
}

// prepareCopy decides whether packet gets a copy in the other codec, marks
// it with HasCopy, and reserves its place on the transcoder that makes the
// copy, which it returns, or nil, and its place among the copies of its
// source slot. A Voice LC Header or Terminator whose LC fails its check has
// no copy, so every target gets it as it came.
func (h *Hub) prepareCopy(ctx context.Context, packet *models.Packet, sc *streamCodec, sourceName string) (*transcoder, *copyTicket) {
	if sc == nil || packet.Codec == uint8(transcode.CodecUnknown) || sc.encrypted || !h.transcoder.Enabled() {
		return nil, nil
	}
	if isVoiceLC(packet) && !transcode.LCValid(packet) {
		return nil, nil
	}
	now := time.Now()
	if sc.needAt.IsZero() || now.Sub(sc.needAt) >= needTTL {
		sc.need = h.needsCopy(ctx, packet, sc, sourceName)
		sc.needAt = now
	}
	if !sc.need {
		return nil, nil
	}
	tx := sc.transcoder()
	if tx == nil || !tx.reserve() {
		mode := codec2.ModeAuto
		if sc.codec == transcode.CodecAMBE {
			mode = h.copyMode(ctx, packet)
		}
		st, err := h.transcoder.NewStream(sc.codec, sc.codec.Other(), mode)
		if err != nil {
			slog.Error("Cannot convert a call to the other codec", "stream", packet.StreamID, "error", err)
			sc.need = false
			return nil, nil
		}
		tx = h.startTranscoder(ctx, st)
		tx.reserve()
		sc.setTranscoder(tx)
	}
	packet.HasCopy = true
	return tx, h.reserveCopy(h.sourceSlotOf(*packet, sourceName), sourceName)
}

// copyOrder holds the places of the copies of one source slot, in the
// order of their originals, so that the copies of a stream go out before
// those of the next stream of the source, as the originals do.
type copyOrder struct {
	queue    []*copyTicket
	emitting bool
}

// copyTicket is the place of the copies of one burst: its copy, after the
// fills of the bursts lost before it. packets is empty when the burst gets
// no copy.
type copyTicket struct {
	key     sourceSlot
	source  string
	done    bool
	packets []models.Packet
}

func (h *Hub) reserveCopy(key sourceSlot, sourceName string) *copyTicket {
	t := &copyTicket{key: key, source: sourceName}
	h.copyMu.Lock()
	defer h.copyMu.Unlock()
	o, ok := h.copyOrders[key]
	if !ok {
		o = &copyOrder{}
		h.copyOrders[key] = o
	}
	o.queue = append(o.queue, t)
	return t
}

// finishCopy sets the copies of t, none when packets is empty, and routes
// the copies of its source slot that are next in order.
func (h *Hub) finishCopy(ctx context.Context, t *copyTicket, packets ...models.Packet) {
	if t == nil {
		return
	}
	h.copyMu.Lock()
	t.done, t.packets = true, packets
	o := h.copyOrders[t.key]
	if o == nil || o.emitting {
		h.copyMu.Unlock()
		return
	}
	o.emitting = true
	for {
		var ready []*copyTicket
		for len(o.queue) > 0 && o.queue[0].done {
			ready = append(ready, o.queue[0])
			o.queue = o.queue[1:]
		}
		if len(ready) == 0 {
			o.emitting = false
			if len(o.queue) == 0 {
				delete(h.copyOrders, t.key)
			}
			h.copyMu.Unlock()
			return
		}
		h.copyMu.Unlock()
		for _, r := range ready {
			for _, p := range r.packets {
				h.fanOut(ctx, p, r.source)
			}
		}
		h.copyMu.Lock()
	}
}

// needsCopy reports whether a target of packet uses the other codec than
// the stream: for an AMBE call a Codec2 timeslot, for a Codec2 call an
// AMBE timeslot, IPSC repeater or OpenBridge peer that does not carry
// Codec2.
func (h *Hub) needsCopy(ctx context.Context, packet *models.Packet, sc *streamCodec, sourceName string) bool {
	want := sc.codec.Other().String()
	if sc.codec == transcode.CodecCodec2 && h.toPeers(sourceName) && h.anyAMBEPeer(ctx, *packet) {
		return true
	}
	if !packet.GroupCall {
		rpt, ok := h.privateTargetRepeater(ctx, *packet)
		return ok && rpt.SlotCodec(packet.Slot) == want
	}
	var ids []uint
	if sc.codec == transcode.CodecAMBE {
		ids = h.codec2Repeaters()
	} else {
		ids = h.talkgroupRepeaters(packet.Dst)
	}
	for _, id := range ids {
		rpt, err := h.routingRepeater(id)
		if err != nil {
			continue
		}
		if slot, ok := groupTargetSlot(rpt, packet); ok && rpt.SlotCodec(slot) == want {
			return true
		}
	}
	return false
}

// groupTargetSlot returns the timeslot on which rpt gets the group burst
// packet, as the subscription manager delivers it, and whether rpt gets
// it.
func groupTargetSlot(rpt *models.Repeater, packet *models.Packet) (slot bool, ok bool) {
	if packet.Repeater == rpt.ID {
		return !packet.Slot, rpt.SimplexRepeater
	}
	ok, slot = rpt.WantRX(*packet)
	return slot, ok
}

// copyMode returns the Codec2 mode for the copy of an AMBE call: the mode
// last heard on the first Codec2 timeslot that takes it, Mode2400 when
// none was heard. All Codec2 targets of a call share one copy.
func (h *Hub) copyMode(ctx context.Context, packet *models.Packet) codec2.Mode {
	if !packet.GroupCall {
		if rpt, ok := h.privateTargetRepeater(ctx, *packet); ok {
			return modeOf(rpt.SlotCodec2Mode(packet.Slot))
		}
		return codec2.Mode2400
	}
	for _, id := range h.codec2Repeaters() {
		rpt, err := h.routingRepeater(id)
		if err != nil {
			continue
		}
		if slot, ok := groupTargetSlot(rpt, packet); ok && rpt.SlotCodec(slot) == models.CodecCodec2 {
			return modeOf(rpt.SlotCodec2Mode(slot))
		}
	}
	return codec2.Mode2400
}

func modeOf(m int) codec2.Mode {
	if m == int(codec2.Mode3200) {
		return codec2.Mode3200
	}
	return codec2.Mode2400
}

// toPeers reports whether the bursts of sourceName go to OpenBridge
// peers.
func (h *Hub) toPeers(sourceName string) bool {
	if !h.hasPeerServers() {
		return false
	}
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin {
		return true
	}
	return h.getServerRole(sourceName) != RolePeer
}

func (h *Hub) anyAMBEPeer(ctx context.Context, packet models.Packet) bool {
	set, ok := h.peerSet(ctx, packet)
	if !ok {
		return false
	}
	for _, p := range set.peers {
		if !p.codec2 {
			return true
		}
	}
	return false
}

func (h *Hub) talkgroupRepeaters(tg uint) []uint {
	ids, err := h.tgRepeaters.Get(tg)
	if err != nil {
		slog.Error("Cannot list the repeaters of a talkgroup", "talkgroupID", tg, "error", err)
		return nil
	}
	return ids
}

func (h *Hub) codec2Repeaters() []uint {
	if p := h.codec2Slots.Load(); p != nil {
		return *p
	}
	return h.loadCodec2Repeaters()
}

func (h *Hub) loadCodec2Repeaters() []uint {
	ids, err := models.Codec2RepeaterIDs(h.db)
	if err != nil {
		slog.Error("Cannot list the repeaters with a Codec2 timeslot", "error", err)
		return nil
	}
	h.codec2Slots.Store(&ids)
	return ids
}

// wantsVersion reports whether a target on the codec target takes packet:
// the original when it is in the codec of the target or has no copy, the
// copy when the copy is in that codec.
func wantsVersion(packet *models.Packet, target string) bool {
	c := transcode.Codec(packet.Codec)
	if c == transcode.CodecUnknown {
		return !packet.Copy
	}
	t := transcode.ParseCodec(target)
	if packet.Copy {
		return c == t
	}
	return c == t || !packet.HasCopy
}

// wantsPeerVersion is wantsVersion for an OpenBridge peer: a peer that
// carries Codec2 gets every call in its own codec, any other peer AMBE.
func wantsPeerVersion(packet *models.Packet, codec2Peer bool) bool {
	if codec2Peer {
		return !packet.Copy
	}
	return wantsVersion(packet, models.CodecAMBE)
}

// noteCopy tells the call tracker, once per stream, that target got the
// copy packet.
func (h *Hub) noteCopy(packet *models.Packet, target string) {
	if !packet.Copy || h.callTracker == nil {
		return
	}
	if _, seen := h.copyReports.LoadOrStore(copyReport{stream: packet.StreamID, target: target}, time.Now()); seen {
		return
	}
	h.callTracker.Converted(*packet, transcode.Codec(packet.Codec).String(), target)
}

// transcoder makes the copies of one stream in the other codec, in the
// order of its bursts, and routes them like the original, after the copies
// of the streams before it from the same source slot. It counts as a call
// in flight until it ends, so that shutdown waits for the copies.
type transcoder struct {
	h      *Hub
	st     *transcode.Stream
	in     chan transcodeJob
	quit   chan struct{}
	once   sync.Once
	marked bool

	mu      sync.Mutex
	pending int
	closed  bool
	exited  bool
}

type transcodeJob struct {
	packet   models.Packet
	deadline time.Time
	ticket   *copyTicket
}

func (h *Hub) startTranscoder(ctx context.Context, st *transcode.Stream) *transcoder {
	t := &transcoder{h: h, st: st, in: make(chan transcodeJob, queueLen(st.Budget())), quit: make(chan struct{})}
	h.transcoders.Add()
	h.calls.Add()
	go t.run(context.WithoutCancel(ctx))
	return t
}

// reserve claims the place of the next burst, which queue then hands
// over. It reports false when the transcoder takes no more bursts.
func (t *transcoder) reserve() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	t.pending++
	return true
}

// queue hands the transcoder the burst that reserve claimed, with its
// place among the copies of its source slot. A burst over the queue, or
// one that comes after the transcoder ended, is not converted and counts
// as missed.
func (t *transcoder) queue(ctx context.Context, p models.Packet, ticket *copyTicket) {
	t.mu.Lock()
	t.pending--
	if t.exited {
		t.missed(&p, voiceFrames(&p), transcode.ReasonLate)
		t.mu.Unlock()
		t.h.finishCopy(ctx, ticket)
		return
	}
	select {
	case t.in <- transcodeJob{packet: p, deadline: time.Now().Add(t.st.Budget()), ticket: ticket}:
		t.mu.Unlock()
	default:
		t.missed(&p, voiceFrames(&p), transcode.ReasonLate)
		t.mu.Unlock()
		t.h.finishCopy(ctx, ticket)
	}
}

// stop makes the transcoder take no more bursts, convert those it has and
// end.
func (t *transcoder) stop() {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	t.once.Do(func() { close(t.quit) })
}

// idleEnd ends the transcoder after streamIdle without a burst, unless a
// burst is on its way.
func (t *transcoder) idleEnd() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending > 0 || len(t.in) > 0 {
		return false
	}
	t.closed = true
	return true
}

func (t *transcoder) next() (transcodeJob, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case job := <-t.in:
		return job, true
	default:
		return transcodeJob{}, false
	}
}

func (t *transcoder) exit(ctx context.Context) {
	t.mu.Lock()
	t.closed, t.exited = true, true
	var dropped []*copyTicket
	for len(t.in) > 0 {
		job := <-t.in
		t.missed(&job.packet, voiceFrames(&job.packet), transcode.ReasonLate)
		dropped = append(dropped, job.ticket)
	}
	t.mu.Unlock()
	for _, ticket := range dropped {
		t.h.finishCopy(ctx, ticket)
	}
	t.st.CloseContext(ctx)
	t.h.calls.Done()
	t.h.transcoders.Done()
}

func (t *transcoder) run(ctx context.Context) {
	defer t.exit(ctx)
	idle := time.NewTimer(streamIdle)
	defer idle.Stop()
	for {
		select {
		case <-t.h.done:
			return
		case <-idle.C:
			if t.idleEnd() {
				return
			}
			idle.Reset(streamIdle)
		case job := <-t.in:
			t.convert(ctx, &job)
			idle.Reset(streamIdle)
		case <-t.quit:
			for {
				job, ok := t.next()
				if !ok {
					return
				}
				t.convert(ctx, &job)
			}
		}
	}
}

func (t *transcoder) convert(ctx context.Context, job *transcodeJob) {
	res, ok := t.st.Convert(ctx, &job.packet, job.deadline)
	if !ok {
		t.h.finishCopy(ctx, job.ticket)
		return
	}
	if res.Busy {
		t.busy(&job.packet, res.Missed)
		t.h.finishCopy(ctx, job.ticket)
		return
	}
	t.reports(&job.packet, &res)
	cp := job.packet
	cp.DMRData = res.Burst
	cp.Codec = uint8(t.st.To())
	cp.Copy, cp.HasCopy = true, false
	if res.Missed > 0 {
		t.missed(&cp, res.Missed, res.Reason)
	}
	copies := make([]models.Packet, 0, len(res.Fill)+1)
	for _, f := range res.Fill {
		fill := cp
		fill.DMRData, fill.DTypeOrVSeq, fill.Seq = f.Burst, f.Position, f.Seq
		fill.FrameType = dmrconst.FrameVoice
		if f.Position == 0 {
			fill.FrameType = dmrconst.FrameVoiceSync
		}
		copies = append(copies, fill)
	}
	t.h.finishCopy(ctx, job.ticket, append(copies, cp)...)
}

// reports counts the frame reports of the decoder on the burst p in the
// metrics, and logs the frames that were not good voice.
func (t *transcoder) reports(p *models.Packet, res *transcode.Result) {
	from := t.st.From().String()
	m := metrics.NewMetrics()
	for i, r := range res.Frames {
		frame, output := transcode.FrameLabels(r)
		m.IncrementTranscodeFrame(from, frame, output)
		if r.Frame != vocoder.FrameGood || r.HighErrorRate {
			slog.Debug("The decoder reported a frame of a converted call", "stream", p.StreamID, "from", from, "frame", frame, "output", output, "index", i, "fills", len(res.Fill), "highErrorRate", r.HighErrorRate)
		}
	}
}

// busy counts a burst of a call that found every AMBE server busy, which
// is not converted, and marks the call once.
func (t *transcoder) busy(p *models.Packet, frames int) {
	if !t.marked {
		t.marked = true
		slog.Info("Every AMBE server is busy, a call is not converted", "stream", p.StreamID, "to", t.st.To().String())
		if t.h.callTracker != nil {
			t.h.callTracker.Busy(*p)
		}
	}
	if frames > 0 {
		d := time.Duration(frames) * transcode.FrameDuration
		metrics.NewMetrics().AddTranscodeMissed(t.st.To().String(), transcode.ReasonBusy, d.Seconds())
	}
}

func (t *transcoder) missed(p *models.Packet, frames int, reason string) {
	if frames == 0 {
		return
	}
	d := time.Duration(frames) * transcode.FrameDuration
	metrics.NewMetrics().AddTranscodeMissed(t.st.To().String(), reason, d.Seconds())
	if t.h.callTracker != nil {
		t.h.callTracker.Missed(*p, int(d.Milliseconds()))
	}
}

func voiceFrames(p *models.Packet) int {
	if transcode.IsVoice(p) {
		return 3
	}
	return 0
}
