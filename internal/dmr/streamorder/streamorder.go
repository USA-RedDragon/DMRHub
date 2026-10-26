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

// Package streamorder runs work in the order it is queued for each key,
// while the work of different keys runs in parallel.
package streamorder

import (
	"context"
	"sync"
)

// Queue runs the work of each key on a worker of its own, in the order it
// was queued. A slow key never delays another key, and Do never blocks.
type Queue[K comparable] struct {
	mu      sync.Mutex
	workers map[K]chan func()
	depth   int
	done    <-chan struct{}
}

// New makes a Queue that holds up to depth jobs for each key. The workers
// stop when done is closed.
func New[K comparable](depth int, done <-chan struct{}) *Queue[K] {
	return &Queue[K]{workers: map[K]chan func(){}, depth: max(depth, 1), done: done}
}

// Do queues fn on the worker of key. It returns false, without queueing
// fn, when the key already holds depth jobs, when ctx is done, or when the
// Queue is stopped.
func (q *Queue[K]) Do(ctx context.Context, key K, fn func()) bool {
	select {
	case <-ctx.Done():
		return false
	case <-q.done:
		return false
	default:
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	jobs, ok := q.workers[key]
	if !ok {
		jobs = make(chan func(), q.depth)
		q.workers[key] = jobs
		go q.run(key, jobs)
	}
	select {
	case jobs <- fn:
		return true
	default:
		return false
	}
}

func (q *Queue[K]) run(key K, jobs chan func()) {
	for {
		select {
		case <-q.done:
			return
		case fn := <-jobs:
			fn()
			continue
		default:
		}
		q.mu.Lock()
		if len(jobs) == 0 {
			delete(q.workers, key)
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()
	}
}
