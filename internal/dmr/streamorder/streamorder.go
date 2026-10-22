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

// Package streamorder runs work in the order it is queued for each stream,
// while the work of different streams runs in parallel.
package streamorder

import "context"

// Queue runs queued work on a fixed set of workers. All work for one stream
// goes to one worker, which runs it in the order it was queued.
type Queue struct {
	shards []chan func()
	done   <-chan struct{}
}

// New starts a Queue with the given number of workers, each with room for
// depth queued jobs. The workers stop when done is closed.
func New(workers, depth int, done <-chan struct{}) *Queue {
	q := &Queue{shards: make([]chan func(), max(workers, 1)), done: done}
	for i := range q.shards {
		q.shards[i] = make(chan func(), depth)
		go q.run(q.shards[i])
	}
	return q
}

// Do queues fn on the worker of stream. It blocks while that worker is full.
// It returns false, without queueing fn, when ctx is done or the Queue is
// stopped first.
func (q *Queue) Do(ctx context.Context, stream uint, fn func()) bool {
	select {
	case <-ctx.Done():
		return false
	case <-q.done:
		return false
	default:
	}
	select {
	case q.shards[stream%uint(len(q.shards))] <- fn:
		return true
	case <-ctx.Done():
		return false
	case <-q.done:
		return false
	}
}

func (q *Queue) run(shard <-chan func()) {
	for {
		select {
		case <-q.done:
			return
		case fn := <-shard:
			fn()
		}
	}
}
