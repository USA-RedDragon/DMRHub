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

// Package outbox publishes to pubsub, and makes other calls to Redis, on
// goroutines of its own, in the order of the calls of each key, so that
// the callers never wait on Redis.
package outbox

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/monotime"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

// Deep is the number of jobs in the queue of one key from which the
// outbox warns that Redis does not take them: about a minute of the
// bursts of one slot.
const Deep = 1000

const (
	deepLogEvery  = 10 * time.Second
	errorLogEvery = time.Second
	retryFirst    = 50 * time.Millisecond
	retryMax      = time.Second
	compactAt     = 1024
)

// Job is one call to make: a publish of Data on Topic, or, when Send is
// set, Send, which Topic names in the logs.
type Job struct {
	Topic string
	Data  []byte
	Send  func() error
	// Repeatable tells that Send has the same result when it runs twice,
	// as a write of a key does, so that the outbox runs it again after
	// any error. Other jobs run again only after an error that wraps
	// pubsub.ErrNotSent.
	Repeatable bool

	queued int64
}

// Outbox runs the jobs of each key in the order they were added, on a
// goroutine of its own while the key has jobs. It drops no job: the queue
// of a key has no limit and keeps a job that did not reach Redis until
// Redis takes it. The dmr_outbox_bursts gauge is the number of jobs in all
// outboxes, and an outbox warns when the queue of a key is deep.
type Outbox[K comparable] struct {
	ps      pubsub.PubSub
	done    <-chan struct{}
	what    string
	attrs   func(K) []any
	mu      sync.Mutex
	queues  map[K]*queue
	depth   atomic.Int64
	pending pending
	deepLog limitedLog
	errLog  limitedLog
}

type queue struct {
	jobs []Job
	head int
}

// New returns an outbox that publishes to ps until done is closed. what
// names its jobs in the logs, such as "the bursts of a source slot", and
// attrs gives the log attributes of a key.
func New[K comparable](ps pubsub.PubSub, done <-chan struct{}, what string, attrs func(K) []any) *Outbox[K] {
	return &Outbox[K]{ps: ps, done: done, what: what, attrs: attrs, queues: map[K]*queue{}}
}

// Add queues job on the queue of key. It never blocks.
func (o *Outbox[K]) Add(key K, job Job) {
	select {
	case <-o.done:
		slog.Debug("Stopping, not running a job of the outbox", "what", o.what, "topic", job.Topic)
		return
	default:
	}
	job.queued = monotime.Now()
	o.pending.add()
	o.depth.Add(1)
	metrics.NewMetrics().OutboxBursts.Inc()
	o.mu.Lock()
	q, ok := o.queues[key]
	if !ok {
		q = &queue{}
		o.queues[key] = q
		go o.run(key, q)
	}
	q.jobs = append(q.jobs, job)
	n := len(q.jobs) - q.head
	oldest := q.jobs[q.head].queued
	o.mu.Unlock()
	if n >= Deep {
		if unlogged, ok := o.deepLog.allow(deepLogEvery); ok {
			attrs := append(o.attrs(key), "jobs", n, "oldest", time.Duration(job.queued-oldest), "allKeys", o.depth.Load(), "unloggedWarnings", unlogged)
			slog.Warn(fmt.Sprintf("Redis is not taking %s, holding them in memory", o.what), attrs...)
		}
	}
}

// Depth returns the number of jobs in the outbox.
func (o *Outbox[K]) Depth() int64 {
	return o.depth.Load()
}

// Wait waits until the outbox has no job, or until done is closed and
// the jobs that run end.
func (o *Outbox[K]) Wait() {
	o.pending.wait()
}

func (o *Outbox[K]) run(key K, q *queue) {
	for {
		o.mu.Lock()
		if q.head == len(q.jobs) {
			delete(o.queues, key)
			o.mu.Unlock()
			return
		}
		job := q.jobs[q.head]
		q.jobs[q.head] = Job{}
		q.head++
		switch {
		case q.head == len(q.jobs):
			q.jobs, q.head = q.jobs[:0], 0
		case q.head >= compactAt && 2*q.head >= len(q.jobs):
			n := copy(q.jobs, q.jobs[q.head:])
			clear(q.jobs[n:])
			q.jobs, q.head = q.jobs[:n], 0
		}
		o.mu.Unlock()
		o.publish(job)
		o.depth.Add(-1)
		metrics.NewMetrics().OutboxBursts.Dec()
		o.pending.done()
	}
}

// publish runs job until Redis takes it. A job that failed after it may
// have reached Redis is not run again, unless it is repeatable, since
// Redis may still run it, and a second publish could deliver it twice.
func (o *Outbox[K]) publish(job Job) {
	wait := retryFirst
	for {
		var err error
		if job.Send != nil {
			err = job.Send()
		} else {
			err = o.ps.Publish(job.Topic, job.Data)
		}
		if err == nil {
			return
		}
		if !job.Repeatable && !errors.Is(err, pubsub.ErrNotSent) {
			metrics.NewMetrics().PublishUnconfirmedTotal.Inc()
			if unlogged, ok := o.errLog.allow(errorLogEvery); ok {
				slog.Warn("A job of the outbox failed and does not run again, since Redis may have it", "what", o.what, "topic", job.Topic, "error", err, "unloggedErrors", unlogged)
			}
			return
		}
		metrics.NewMetrics().PublishRetriesTotal.Inc()
		if unlogged, ok := o.errLog.allow(errorLogEvery); ok {
			slog.Warn("A job of the outbox did not reach Redis, running it again", "what", o.what, "topic", job.Topic, "in", wait, "error", err, "unloggedErrors", unlogged)
		}
		t := time.NewTimer(wait)
		select {
		case <-o.done:
			t.Stop()
			slog.Warn("Stopped before Redis took a job of the outbox", "what", o.what, "topic", job.Topic)
			return
		case <-t.C:
		}
		wait = min(2*wait, retryMax)
	}
}

// pending counts the jobs in an outbox. Unlike a sync.WaitGroup, add may
// run while wait waits.
type pending struct {
	mu   sync.Mutex
	n    int
	zero chan struct{}
}

func (p *pending) add() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n == 0 {
		p.zero = make(chan struct{})
	}
	p.n++
}

func (p *pending) done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n--
	if p.n == 0 {
		close(p.zero)
	}
}

func (p *pending) wait() {
	p.mu.Lock()
	if p.n == 0 {
		p.mu.Unlock()
		return
	}
	zero := p.zero
	p.mu.Unlock()
	<-zero
}

// limitedLog lets a log through at most once per period, and counts the
// logs that it held back since.
type limitedLog struct {
	next     atomic.Int64
	unlogged atomic.Uint64
}

func (l *limitedLog) allow(every time.Duration) (uint64, bool) {
	now := monotime.Now()
	next := l.next.Load()
	if now < next || !l.next.CompareAndSwap(next, now+int64(every)) {
		l.unlogged.Add(1)
		return 0, false
	}
	return l.unlogged.Swap(0), true
}
