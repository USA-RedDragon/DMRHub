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
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/monotime"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

// outboxDeep is the number of bursts in the outbox of one source slot from
// which the outbox warns that pubsub does not take them: about a minute of
// one slot.
const outboxDeep = 1000

// outboxDeepLogEvery is the shortest time between two warnings of a deep
// outbox.
const outboxDeepLogEvery = 10 * time.Second

// outboxErrorLogEvery is the shortest time between two logs of publishes
// that failed.
const outboxErrorLogEvery = time.Second

// outboxRetryFirst and outboxRetryMax are the first and the longest wait
// before an outbox publishes again a burst that did not reach pubsub.
const (
	outboxRetryFirst = 50 * time.Millisecond
	outboxRetryMax   = time.Second
)

// outboxCompactAt is the number of sent bursts at the head of the queue of
// a source slot from which the outbox moves the rest down, so that a long
// outage does not keep the memory of the bursts already sent.
const outboxCompactAt = 1024

// outbox publishes the routed bursts of each source slot in the order they
// were routed, on a goroutine of its own, so that the routing worker of a
// slot never waits on pubsub. It drops no burst: the queue of a slot has no
// limit and keeps a burst that did not reach pubsub until pubsub takes it.
// A burst in it takes about 350 bytes, so an hour of one busy slot, 60000
// bursts, is about 20 MB. The dmr_outbox_bursts gauge is the number of
// bursts in it, and it warns when the queue of a slot is deep.
type outbox struct {
	ps      pubsub.PubSub
	done    <-chan struct{}
	mu      sync.Mutex
	slots   map[sourceSlot]*outboxSlot
	depth   atomic.Int64
	pending inflight
	deepLog limitedLog
	errLog  limitedLog
}

type outboxSlot struct {
	jobs []outboxJob
	head int
}

// outboxJob is one burst to publish: data on topic, or, when send is set,
// the hand-off that send makes.
type outboxJob struct {
	queued int64
	topic  string
	data   []byte
	send   func() error
}

// limitedLog lets a log through at most once per period, and counts the
// logs that it held back since.
type limitedLog struct {
	next     atomic.Int64
	unlogged atomic.Uint64
}

// allow reports whether a log may go now, with the number of logs held
// back since the last one.
func (l *limitedLog) allow(every time.Duration) (uint64, bool) {
	now := monotime.Now()
	next := l.next.Load()
	if now < next || !l.next.CompareAndSwap(next, now+int64(every)) {
		l.unlogged.Add(1)
		return 0, false
	}
	return l.unlogged.Swap(0), true
}

func newOutbox(ps pubsub.PubSub, done <-chan struct{}) *outbox {
	return &outbox{ps: ps, done: done, slots: map[sourceSlot]*outboxSlot{}}
}

// add queues job on the queue of the source slot key. It never blocks.
func (o *outbox) add(key sourceSlot, job outboxJob) {
	select {
	case <-o.done:
		slog.Debug("Hub stopping, not publishing a burst", "topic", job.topic)
		return
	default:
	}
	job.queued = monotime.Now()
	o.pending.Add()
	o.depth.Add(1)
	metrics.NewMetrics().OutboxBursts.Inc()
	o.mu.Lock()
	s, ok := o.slots[key]
	if !ok {
		s = &outboxSlot{}
		o.slots[key] = s
		go o.run(key, s)
	}
	s.jobs = append(s.jobs, job)
	n := len(s.jobs) - s.head
	oldest := s.jobs[s.head].queued
	o.mu.Unlock()
	if n >= outboxDeep {
		if unlogged, ok := o.deepLog.allow(outboxDeepLogEvery); ok {
			slog.Warn("Pubsub is not taking the bursts of a source slot, holding them in memory",
				"server", key.server, "repeater", key.repeater, "slot", key.slot, "bursts", n,
				"oldest", time.Duration(job.queued-oldest), "allSlots", o.depth.Load(), "unloggedWarnings", unlogged)
		}
	}
}

func (o *outbox) run(key sourceSlot, s *outboxSlot) {
	for {
		o.mu.Lock()
		if s.head == len(s.jobs) {
			delete(o.slots, key)
			o.mu.Unlock()
			return
		}
		job := s.jobs[s.head]
		s.jobs[s.head] = outboxJob{}
		s.head++
		switch {
		case s.head == len(s.jobs):
			s.jobs, s.head = s.jobs[:0], 0
		case s.head >= outboxCompactAt && 2*s.head >= len(s.jobs):
			n := copy(s.jobs, s.jobs[s.head:])
			clear(s.jobs[n:])
			s.jobs, s.head = s.jobs[:n], 0
		}
		o.mu.Unlock()
		o.publish(job)
		o.depth.Add(-1)
		metrics.NewMetrics().OutboxBursts.Dec()
		o.pending.Done()
	}
}

// publish sends job until pubsub takes it. A publish that failed after it
// may have reached pubsub is not sent again, since pubsub may still
// deliver it, and a second publish could deliver it twice.
func (o *outbox) publish(job outboxJob) {
	wait := outboxRetryFirst
	for {
		var err error
		if job.send != nil {
			err = job.send()
		} else {
			err = o.ps.Publish(job.topic, job.data)
		}
		if err == nil {
			return
		}
		if !errors.Is(err, pubsub.ErrNotSent) {
			metrics.NewMetrics().PublishUnconfirmedTotal.Inc()
			if unlogged, ok := o.errLog.allow(outboxErrorLogEvery); ok {
				slog.Warn("A publish of a burst failed and is not sent again, since pubsub may have it", "topic", job.topic, "error", err, "unloggedErrors", unlogged)
			}
			return
		}
		metrics.NewMetrics().PublishRetriesTotal.Inc()
		if unlogged, ok := o.errLog.allow(outboxErrorLogEvery); ok {
			slog.Warn("A burst did not reach pubsub, publishing it again", "topic", job.topic, "in", wait, "error", err, "unloggedErrors", unlogged)
		}
		t := time.NewTimer(wait)
		select {
		case <-o.done:
			t.Stop()
			slog.Warn("Hub stopped before pubsub took a burst", "topic", job.topic)
			return
		case <-t.C:
		}
		wait = min(2*wait, outboxRetryMax)
	}
}
