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
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/USA-RedDragon/DMRHub/internal/metrics"
)

const (
	// maxQueuedPerRepeater is the most jobs that wait for one repeater
	// before Do and After drop a job.
	maxQueuedPerRepeater = 64
	// MaxStateJobsPerRepeater is the most jobs that wait for one repeater
	// before Must and MustAfter refuse a job. Jobs that change the state
	// of a session come only from logins, moves and session ends, so a
	// queue this long means that the stores of the repeater do not answer.
	MaxStateJobsPerRepeater = 16 * maxQueuedPerRepeater
)

// RepeaterQueue runs the jobs of each repeater in order, one repeater
// beside another, so that the KV store, the database and the hub never
// hold up the sessions of the other repeaters. The zero value is ready.
type RepeaterQueue struct {
	mu      sync.Mutex
	idle    *sync.Cond
	jobs    map[uint][]func()
	running int
	dropped atomic.Uint64
}

// Do queues fn after the jobs of the repeater id. A repeater with
// maxQueuedPerRepeater jobs waiting drops fn, and Do reports false. Do is
// for work that a later job does again, such as the refresh of a ping.
func (q *RepeaterQueue) Do(id uint, fn func()) bool {
	return q.add(id, fn, maxQueuedPerRepeater)
}

// After calls fn once the jobs of the repeater id that are queued now are
// done, and waits for it to return. It reports false, without calling fn,
// when the queue of the repeater is full.
func (q *RepeaterQueue) After(id uint, fn func()) bool {
	return q.wait(id, fn, maxQueuedPerRepeater)
}

// Must queues fn, a job that changes the state of a session of the
// repeater id, after the jobs of the repeater. It reports false, without
// queueing fn, only when MaxStateJobsPerRepeater jobs wait; the caller
// then ends the session.
func (q *RepeaterQueue) Must(id uint, fn func()) bool {
	return q.add(id, fn, MaxStateJobsPerRepeater)
}

// MustAfter is Must that waits for fn to return.
func (q *RepeaterQueue) MustAfter(id uint, fn func()) bool {
	return q.wait(id, fn, MaxStateJobsPerRepeater)
}

// Always queues fn, the job that ends a session of the repeater id, after
// the jobs of the repeater, however many wait. A session ends once, so
// these jobs stay few.
func (q *RepeaterQueue) Always(id uint, fn func()) {
	q.add(id, fn, 0)
}

func (q *RepeaterQueue) wait(id uint, fn func(), limit int) bool {
	done := make(chan struct{})
	if !q.add(id, func() {
		defer close(done)
		fn()
	}, limit) {
		return false
	}
	<-done
	return true
}

// add queues fn after the jobs of the repeater id when fewer than limit
// jobs wait, or always when limit is 0.
func (q *RepeaterQueue) add(id uint, fn func(), limit int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.jobs == nil {
		q.jobs = map[uint][]func(){}
	}
	jobs, busy := q.jobs[id]
	if limit > 0 && len(jobs) >= limit {
		q.refuse(id, limit)
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

func (q *RepeaterQueue) refuse(id uint, limit int) {
	if limit == MaxStateJobsPerRepeater {
		metrics.NewMetrics().IncrementRepeaterJobsDropped(metrics.RepeaterJobState)
		slog.Error("Refusing a session job of a repeater whose queue does not move; ending the session", "repeaterID", id, "queued", limit)
		return
	}
	metrics.NewMetrics().IncrementRepeaterJobsDropped(metrics.RepeaterJobBestEffort)
	if n := q.dropped.Add(1); bits.OnesCount64(n) == 1 {
		slog.Warn("Dropping jobs of repeaters with a full queue", "repeaterID", id, "dropped", n)
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
