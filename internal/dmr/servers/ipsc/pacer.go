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
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

const (
	burstTime       = 60 * time.Millisecond
	callHang        = 2 * time.Second
	maxCallWait     = 30 * time.Second
	pacerIdleExpiry = 30 * time.Second

	pacerDrainTimeout = 5 * time.Second
	pacerDrainPoll    = 10 * time.Millisecond
)

type slotKey struct {
	peer uint32
	slot bool
}

type queuedFrame struct {
	frame []byte
	pkt   models.Packet
}

type queuedCall struct {
	stream   uint
	frames   []queuedFrame
	ended    bool
	queuedAt time.Time
	lastSeen time.Time
}

type slotQueue struct {
	active    *queuedCall
	pending   []*queuedCall
	discarded map[uint]time.Time
	wake      chan struct{}
}

func (q *slotQueue) call(stream uint, now time.Time) *queuedCall {
	if q.active != nil && q.active.stream == stream {
		return q.active
	}
	for _, c := range q.pending {
		if c.stream == stream && !c.ended {
			return c
		}
	}
	c := &queuedCall{stream: stream, queuedAt: now}
	if q.active == nil {
		q.active = c
	} else {
		q.pending = append(q.pending, c)
	}
	return c
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
	now     func() time.Time
	mu      sync.Mutex
	slots   map[slotKey]*slotQueue
	done    chan struct{}
	wg      sync.WaitGroup
}

func newPacer(send func(slotKey, []byte), discard func(models.Packet)) *pacer {
	return &pacer{send: send, discard: discard, now: time.Now, slots: map[slotKey]*slotQueue{}, done: make(chan struct{})}
}

func (p *pacer) enqueue(k slotKey, stream uint, frame []byte, end bool, pkt models.Packet) {
	var discarded []models.Packet
	defer func() { p.report(discarded) }()
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return
	default:
	}
	q, ok := p.slots[k]
	if !ok {
		q = &slotQueue{discarded: map[uint]time.Time{}, wake: make(chan struct{}, 1)}
		p.slots[k] = q
		p.wg.Add(1)
		go p.run(k, q)
	}
	now := p.now()
	if at, gone := q.discarded[stream]; gone {
		if now.Sub(at) < callHang {
			q.discarded[stream] = now
			discarded = append(discarded, pkt)
			return
		}
		delete(q.discarded, stream)
	}
	c := q.call(stream, now)
	c.frames = append(c.frames, queuedFrame{frame: frame, pkt: pkt})
	c.lastSeen = now
	if end {
		c.ended = true
	}
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

func (p *pacer) take(q *slotQueue, now time.Time) ([]byte, time.Duration, bool) {
	var discarded []models.Packet
	defer func() { p.report(discarded) }()
	p.mu.Lock()
	defer p.mu.Unlock()
	discarded = q.expire(now)
	for {
		if q.active == nil {
			if len(q.pending) == 0 {
				return nil, 0, false
			}
			q.active = q.pending[0]
			q.pending[0] = nil
			q.pending = q.pending[1:]
		}
		c := q.active
		if len(c.frames) > 0 {
			f := c.frames[0]
			c.frames[0] = queuedFrame{}
			c.frames = c.frames[1:]
			return f.frame, 0, true
		}
		quiet := now.Sub(c.lastSeen)
		if !c.ended && quiet < callHang {
			if len(q.pending) == 0 {
				return nil, 0, false
			}
			return nil, callHang - quiet, false
		}
		q.active = nil
	}
}

func (p *pacer) run(k slotKey, q *slotQueue) {
	defer p.wg.Done()
	idle := time.NewTimer(pacerIdleExpiry)
	defer idle.Stop()
	var next time.Time
	for {
		frame, wait, ok := p.take(q, p.now())
		if !ok {
			if p.idle(k, q, wait, idle) {
				return
			}
			continue
		}
		if wait := time.Until(next); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-p.done:
				t.Stop()
				return
			case <-t.C:
			}
		}
		p.send(k, frame)
		next = time.Now().Add(burstTime)
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
		idle.Reset(pacerIdleExpiry)
	}
}

func (p *pacer) idle(k slotKey, q *slotQueue, wait time.Duration, idle *time.Timer) bool {
	var hang <-chan time.Time
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		hang = t.C
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
