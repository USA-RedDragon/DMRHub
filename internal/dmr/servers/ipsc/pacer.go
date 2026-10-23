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
)

const (
	burstTime       = 60 * time.Millisecond
	pacerIdleExpiry = 30 * time.Second

	pacerDrainTimeout = 5 * time.Second
	pacerDrainPoll    = 10 * time.Millisecond
)

type slotKey struct {
	peer uint32
	slot bool
}

type slotQueue struct {
	frames [][]byte
	wake   chan struct{}
}

type pacer struct {
	send  func(slotKey, []byte)
	mu    sync.Mutex
	slots map[slotKey]*slotQueue
	done  chan struct{}
	wg    sync.WaitGroup
}

func newPacer(send func(slotKey, []byte)) *pacer {
	return &pacer{send: send, slots: map[slotKey]*slotQueue{}, done: make(chan struct{})}
}

func (p *pacer) enqueue(k slotKey, frame []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return
	default:
	}
	q, ok := p.slots[k]
	if !ok {
		q = &slotQueue{wake: make(chan struct{}, 1)}
		p.slots[k] = q
		p.wg.Add(1)
		go p.run(k, q)
	}
	q.frames = append(q.frames, frame)
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (p *pacer) take(k slotKey, q *slotQueue, expire bool) ([]byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(q.frames) == 0 {
		if expire {
			delete(p.slots, k)
		}
		return nil, false
	}
	frame := q.frames[0]
	q.frames[0] = nil
	q.frames = q.frames[1:]
	return frame, true
}

func (p *pacer) run(k slotKey, q *slotQueue) {
	defer p.wg.Done()
	idle := time.NewTimer(pacerIdleExpiry)
	defer idle.Stop()
	var next time.Time
	for {
		frame, ok := p.take(k, q, false)
		if !ok {
			select {
			case <-p.done:
				return
			case <-q.wake:
				continue
			case <-idle.C:
				if _, ok := p.take(k, q, true); !ok {
					return
				}
				idle.Reset(pacerIdleExpiry)
				continue
			}
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
		n += len(q.frames)
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
