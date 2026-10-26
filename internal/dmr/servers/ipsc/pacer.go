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
package ipsc

import (
	"math"
	"slices"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
)

const (
	burstTime       = 60 * time.Millisecond
	maxPacerLag     = 300 * time.Millisecond
	callHang        = 2 * time.Second
	maxCallWait     = 30 * time.Second
	pacerIdleExpiry = 30 * time.Second

	pacerDrainTimeout = 5 * time.Second
	pacerDrainPoll    = 10 * time.Millisecond

	callHangHold     = 2500 * time.Millisecond
	dataHangHold     = 1200 * time.Millisecond
	maxWaitingCalls  = 8
	maxWaitingFrames = int(maxCallWait/burstTime) + 20
)

type slotKey struct {
	peer uint32
	slot bool
}

type queuedFrame struct {
	frame []byte
	pkt   models.Packet
	// state is the state of the translator after the packet, on the last
	// frame of the packet.
	state *streamSnapshot
}

type queuedCall struct {
	stream   uint
	frames   []queuedFrame
	ended    bool
	queuedAt time.Time
	lastSeen time.Time
	// voice is a call whose first burst is voice, and data one with a
	// data header or block.
	voice bool
	data  bool
	// sentAt is when the last frame of the call went out.
	sentAt time.Time
	// src, dst and group are the addresses of the first packet of the
	// call, and preamble is set when its last packet is a preamble CSBK.
	src, dst uint
	group    bool
	preamble bool
	dataEnd  dataEnd
	// last is the last frame of the call, and needsEnd is set when it is a
	// preamble or a data packet in progress, which no frame of the call
	// ended on IPSC.
	last     queuedFrame
	needsEnd bool
}

// holdEnd returns the end of the call hang that c gives its slot when it
// ends at now. A voice call holds the data and CSBK calls after it for
// callHangHold from its terminator, or from now when its terminator was
// lost. A data call holds them for dataHangHold from its last block.
func (c *queuedCall) holdEnd(now time.Time) time.Time {
	var until time.Time
	if c.sentAt.IsZero() {
		return until
	}
	if c.voice {
		end := now
		if c.ended {
			end = c.sentAt
		}
		until = end.Add(callHangHold)
	}
	if c.data {
		until = later(until, c.sentAt.Add(dataHangHold))
	}
	return until
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// burstKind reports whether pkt is a voice burst, voice LC header, PI
// header or terminator, or a data header or rate 1/2, 3/4 or 1 block.
func burstKind(pkt models.Packet) (voice, data bool) {
	switch pkt.FrameType {
	case dmrconst.FrameVoice, dmrconst.FrameVoiceSync:
		return true, false
	case dmrconst.FrameDataSync:
	default:
		return false, false
	}
	if pkt.DTypeOrVSeq > math.MaxUint8 {
		return false, false
	}
	switch enums.DataType(pkt.DTypeOrVSeq) { //nolint:exhaustive
	case enums.DataTypeVoiceLCHeader, enums.DataTypePIHeader, enums.DataTypeTerminatorWithLC:
		return true, false
	case enums.DataTypeDataHeader, enums.DataTypeRate12, enums.DataTypeRate34, enums.DataTypeRate1:
		return false, true
	}
	return false, false
}

type slotQueue struct {
	active    *queuedCall
	pending   []*queuedCall
	discarded map[uint]time.Time
	wake      chan struct{}
	// sent is the last sentKeep packets that went out, oldest first.
	sent []sentPacket
	// sending is the frame that the pacer took and sends, until it is in
	// sent, and sendingCall its call.
	sending     *queuedFrame
	sendingCall *queuedCall
	// holdUntil is the end of the call hang of the peer on the slot, until
	// which data and CSBK calls wait.
	holdUntil time.Time
}

func (q *slotQueue) find(stream uint) *queuedCall {
	if q.active != nil && q.active.stream == stream {
		return q.active
	}
	for _, c := range q.pending {
		if c.stream == stream && !c.ended {
			return c
		}
	}
	return nil
}

// drop removes the waiting call c and returns the packets of its frames.
func (q *slotQueue) drop(c *queuedCall) []models.Packet {
	q.pending = slices.DeleteFunc(q.pending, func(p *queuedCall) bool { return p == c })
	out := make([]models.Packet, 0, len(c.frames))
	for _, f := range c.frames {
		out = append(out, f.pkt)
	}
	return out
}

// next returns the index of the waiting call to start at now, or -1 when
// every waiting call is held by the call hang. A voice call is never held.
func (q *slotQueue) next(now time.Time) int {
	if len(q.pending) == 0 {
		return -1
	}
	if !now.Before(q.holdUntil) {
		return 0
	}
	return slices.IndexFunc(q.pending, func(c *queuedCall) bool { return c.voice })
}

// tail returns the call that the next call on the slot would follow: the
// last waiting call, or the active one when none waits.
func (q *slotQueue) tail() *queuedCall {
	if n := len(q.pending); n > 0 {
		return q.pending[n-1]
	}
	return q.active
}

func (q *slotQueue) frames() int {
	n := 0
	if q.active != nil {
		n += len(q.active.frames)
	}
	for _, c := range q.pending {
		n += len(c.frames)
	}
	return n
}

type pacer struct {
	send    func(slotKey, []byte)
	discard func(models.Packet)
	end     func(slotKey, queuedFrame) []byte
	now     func() time.Time
	after   func(time.Duration) <-chan time.Time
	mu      sync.Mutex
	slots   map[slotKey]*slotQueue
	done    chan struct{}
	wg      sync.WaitGroup
}

func newPacer(send func(slotKey, []byte), discard func(models.Packet)) *pacer {
	return &pacer{send: send, discard: discard, now: time.Now, after: time.After, slots: map[slotKey]*slotQueue{}, done: make(chan struct{})}
}

// enqueue queues frame, of the packet pkt of the stream, to the peer slot
// k. A call ends at a frame with end set, at the last block of its data
// packet, or at its last CSBK. A CSBK or data header of a new stream joins
// the call before it when that call ends with the preamble that announces
// it, so no hold and no bound on the waiting calls splits them.
func (p *pacer) enqueue(k slotKey, stream uint, frame []byte, end bool, pkt models.Packet, state *streamSnapshot) {
	b := decodeDataBurst(pkt)
	var discarded []models.Packet
	defer func() { p.report(discarded) }()
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return
	default:
	}
	q := p.queue(k)
	now := p.now()
	if at, gone := q.discarded[stream]; gone {
		if now.Sub(at) < callHang {
			q.discarded[stream] = now
			discarded = append(discarded, pkt)
			return
		}
		delete(q.discarded, stream)
	}
	voice, data := burstKind(pkt)
	c := q.find(stream)
	if t := q.tail(); c == nil && t != nil && t.joins(pkt, b, now) {
		t.stream = stream
		c = t
	}
	if c == nil {
		if len(q.pending) >= maxWaitingCalls {
			q.discarded[stream] = now
			discarded = append(discarded, pkt)
			return
		}
		c = &queuedCall{stream: stream, queuedAt: now, voice: voice, src: pkt.Src, dst: pkt.Dst, group: pkt.GroupCall}
		if q.active == nil && len(q.pending) == 0 && (voice || !now.Before(q.holdUntil)) {
			q.active = c
		} else {
			q.pending = append(q.pending, c)
		}
	}
	if c != q.active && len(c.frames) >= maxWaitingFrames {
		q.discarded[stream] = now
		discarded = append(q.drop(c), pkt)
		return
	}
	c.data = c.data || data
	c.frames = append(c.frames, queuedFrame{frame: frame, pkt: pkt, state: state})
	c.lastSeen = now
	c.preamble = b != nil && isPreamble(b)
	if b != nil {
		if e, known := c.dataEnd.ends(b); known {
			c.ended = e
		}
	}
	if end {
		c.ended = true
	}
	switch {
	case end || voice:
		c.needsEnd = false
	case b != nil:
		c.needsEnd = c.preamble || !c.ended
	}
	c.last = queuedFrame{frame: frame, pkt: pkt}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// queue returns the queue of k, and starts its sender when it is new. The
// caller holds p.mu.
func (p *pacer) queue(k slotKey) *slotQueue {
	q, ok := p.slots[k]
	if !ok {
		q = &slotQueue{discarded: map[uint]time.Time{}, wake: make(chan struct{}, 1)}
		p.slots[k] = q
		p.wg.Add(1)
		go p.run(k, q)
	}
	return q
}

// holdFor holds the data and CSBK calls to the peer slot k for left, the
// rest of a call hang that another replica saw begin.
func (p *pacer) holdFor(k slotKey, left time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return
	default:
	}
	q := p.queue(k)
	q.holdUntil = later(q.holdUntil, p.now().Add(left))
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (p *pacer) report(pkts []models.Packet) {
	if p.discard == nil {
		return
	}
	for _, pkt := range pkts {
		p.discard(pkt)
	}
}

func (q *slotQueue) expire(now time.Time) []models.Packet {
	for _, c := range q.pending {
		if now.Sub(c.queuedAt) > maxCallWait {
			q.discarded[c.stream] = now
		}
	}
	var out []models.Packet
	kept := q.pending[:0]
	for _, c := range q.pending {
		if _, gone := q.discarded[c.stream]; !gone {
			kept = append(kept, c)
			continue
		}
		for _, f := range c.frames {
			out = append(out, f.pkt)
		}
	}
	clear(q.pending[len(kept):])
	q.pending = kept
	for stream, at := range q.discarded {
		if now.Sub(at) >= callHang {
			delete(q.discarded, stream)
		}
	}
	return out
}

// take returns the next frame to send on the peer slot k at now, or how
// long to wait for one. A call whose last frame is a preamble or a block of
// a data packet in progress ends on IPSC with the frame of p.end when
// nothing of it comes for preambleGap, or at once when it is a preamble
// and another call waits.
func (p *pacer) take(k slotKey, q *slotQueue, now time.Time) (queuedFrame, time.Duration, bool) {
	var discarded []models.Packet
	defer func() { p.report(discarded) }()
	p.mu.Lock()
	defer p.mu.Unlock()
	discarded = q.expire(now)
	for {
		if q.active == nil {
			i := q.next(now)
			if i < 0 {
				if len(q.pending) == 0 {
					return queuedFrame{}, 0, false
				}
				return queuedFrame{}, q.holdUntil.Sub(now), false
			}
			q.active = q.pending[i]
			q.pending = slices.Delete(q.pending, i, i+1)
		}
		c := q.active
		if len(c.frames) > 0 {
			f := c.frames[0]
			c.frames[0] = queuedFrame{}
			c.frames = c.frames[1:]
			q.sending = &f
			q.sendingCall = c
			return f, 0, true
		}
		quiet := now.Sub(c.lastSeen)
		if c.needsEnd && p.end != nil {
			if quiet < preambleGap && (!c.preamble || len(q.pending) == 0) {
				return queuedFrame{}, preambleGap - quiet, false
			}
			c.needsEnd, c.ended = false, true
			if frame := p.end(k, c.last); frame != nil {
				f := queuedFrame{frame: frame, pkt: c.last.pkt}
				q.sending = &f
				q.sendingCall = c
				return f, 0, true
			}
		}
		if !c.ended && quiet < callHang {
			if len(q.pending) == 0 {
				return queuedFrame{}, 0, false
			}
			return queuedFrame{}, callHang - quiet, false
		}
		q.holdUntil = later(q.holdUntil, c.holdEnd(now))
		q.active = nil
	}
}

func (p *pacer) run(k slotKey, q *slotQueue) {
	defer p.wg.Done()
	idle := time.NewTimer(pacerIdleExpiry)
	defer idle.Stop()
	var next time.Time
	onGrid := false
	for {
		if wait := time.Until(next); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-p.done:
				t.Stop()
				return
			case <-t.C:
			}
		}
		frame, wait, ok := p.take(k, q, p.now())
		if !ok {
			onGrid = false
			if p.idle(k, q, wait, idle) {
				return
			}
			continue
		}
		now := time.Now()
		if !onGrid || now.Sub(next) > maxPacerLag {
			next = now
		}
		onGrid = true
		p.send(k, frame.frame)
		p.sent(q, frame)
		next = next.Add(burstTime)
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
		idle.Reset(pacerIdleExpiry)
	}
}

// sent remembers f, which went out, when it is the last frame of its
// packet.
func (p *pacer) sent(q *slotQueue, f queuedFrame) {
	p.mu.Lock()
	defer p.mu.Unlock()
	q.sending = nil
	if c := q.sendingCall; c != nil {
		c.sentAt = p.now()
		q.sendingCall = nil
	}
	if f.state == nil {
		return
	}
	if len(q.sent) == sentKeep {
		q.sent = q.sent[1:]
	}
	q.sent = append(q.sent, sentPacket{at: time.Now(), pkt: f.pkt, state: *f.state})
}

// handOver returns, for each call that the pacer sent to the peer, the
// state after the last packet that went out hub.MoveSlack before since,
// and the packets that went out after it or not yet, in order, and the
// rest of the call hang of each slot. It drops what it holds for the peer,
// which moved to another replica.
func (p *pacer) handOver(peer uint32, since time.Time) ([]handedCall, []models.Packet, []handedHang) {
	since = since.Add(-hub.MoveSlack)
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	var calls []handedCall
	var lost []models.Packet
	var hangs []handedHang
	for _, slot := range []bool{false, true} {
		q, ok := p.slots[slotKey{peer: peer, slot: slot}]
		if !ok {
			continue
		}
		until := q.holdUntil
		if q.active != nil {
			until = later(until, q.active.holdEnd(now))
		}
		if left := until.Sub(now); left > 0 {
			hangs = append(hangs, handedHang{Slot: slot, Left: left})
		}
		last := map[uint]sentPacket{}
		var order []uint
		for _, s := range q.sent {
			if !s.at.Before(since) {
				lost = append(lost, s.pkt)
				continue
			}
			if _, seen := last[s.pkt.StreamID]; !seen {
				order = append(order, s.pkt.StreamID)
			}
			last[s.pkt.StreamID] = s
		}
		for _, stream := range order {
			s := last[stream]
			calls = append(calls, handedCall{Slot: slot, Stream: stream, Seq: s.pkt.Seq, State: s.state})
		}
		if q.sending != nil && q.sending.state != nil {
			lost = append(lost, q.sending.pkt)
		}
		q.sending = nil
		for _, c := range append([]*queuedCall{q.active}, q.pending...) {
			if c == nil {
				continue
			}
			for _, f := range c.frames {
				if f.state != nil {
					lost = append(lost, f.pkt)
				}
			}
			c.frames = nil
			c.ended = true
			c.needsEnd = false
		}
		q.sent = nil
	}
	return calls, lost, hangs
}

func (p *pacer) idle(k slotKey, q *slotQueue, wait time.Duration, idle *time.Timer) bool {
	var hang <-chan time.Time
	if wait > 0 {
		hang = p.after(wait)
	}
	select {
	case <-p.done:
		return true
	case <-q.wake:
	case <-hang:
	case <-idle.C:
		p.mu.Lock()
		defer p.mu.Unlock()
		if q.frames() == 0 && len(q.pending) == 0 && len(q.discarded) == 0 {
			delete(p.slots, k)
			return true
		}
		idle.Reset(pacerIdleExpiry)
	}
	return false
}

func (p *pacer) drain(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && p.queued() > 0 {
		time.Sleep(pacerDrainPoll)
	}
	p.stop()
}

func (p *pacer) queued() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, q := range p.slots {
		n += q.frames()
	}
	return n
}

func (p *pacer) stop() {
	p.mu.Lock()
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	p.mu.Unlock()
	p.wg.Wait()
}
