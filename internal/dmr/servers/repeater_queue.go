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

package servers

import (
	"log/slog"
	"sync"
)

// maxQueuedPerRepeater is the most jobs that wait for one repeater.
const maxQueuedPerRepeater = 64

// RepeaterQueue runs the jobs of each repeater in order, one repeater
// beside another, so that the KV store, the database and the hub never
// hold up the sessions of the other repeaters. The zero value is ready.
type RepeaterQueue struct {
	mu      sync.Mutex
	idle    *sync.Cond
	jobs    map[uint][]func()
	running int
}

// Do queues fn after the jobs of the repeater id. A repeater with
// maxQueuedPerRepeater jobs waiting drops fn.
func (q *RepeaterQueue) Do(id uint, fn func()) {
	q.add(id, fn)
}

// After calls fn once the jobs of the repeater id that are queued now are
// done, and waits for it to return. It reports false, without calling fn,
// when the queue of the repeater is full.
func (q *RepeaterQueue) After(id uint, fn func()) bool {
	done := make(chan struct{})
	if !q.add(id, func() {
		defer close(done)
		fn()
	}) {
		return false
	}
	<-done
	return true
}

func (q *RepeaterQueue) add(id uint, fn func()) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.jobs == nil {
		q.jobs = map[uint][]func(){}
	}
	jobs, busy := q.jobs[id]
	if len(jobs) >= maxQueuedPerRepeater {
		slog.Warn("Dropping a job of a repeater with a full queue", "repeaterID", id)
		return false
	}
	q.jobs[id] = append(jobs, fn)
	if !busy {
		q.running++
		go q.run(id)
	}
	return true
}

func (q *RepeaterQueue) run(id uint) {
	for {
		q.mu.Lock()
		jobs := q.jobs[id]
		if len(jobs) == 0 {
			delete(q.jobs, id)
			q.running--
			if q.running == 0 && q.idle != nil {
				q.idle.Broadcast()
			}
			q.mu.Unlock()
			return
		}
		fn := jobs[0]
		q.jobs[id] = jobs[1:]
		q.mu.Unlock()
		fn()
	}
}

// Wait blocks until no job is queued or running.
func (q *RepeaterQueue) Wait() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.idle == nil {
		q.idle = sync.NewCond(&q.mu)
	}
	for q.running > 0 {
		q.idle.Wait()
	}
}
