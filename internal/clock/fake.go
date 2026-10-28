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

// Package monotime keeps a time in an int64 with its monotonic clock

package clock

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Fake is a Clock whose time moves only when Advance moves it. Advance
// fires the timers in the order of their times, in the goroutine that
// calls it: the function of an AfterFunc timer runs before Advance goes
// on, and the channel of a timer or ticker gets its value without
// blocking, as a time.Ticker drops the ticks that nobody takes.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	seq     uint64
	armed   []*fakeTimer
	changed chan struct{}
}

type fakeTimer struct {
	f      *Fake
	at     time.Time
	seq    uint64
	period time.Duration
	ch     chan time.Time
	fn     func()
	armed  bool
}

// NewFake returns a Fake that starts at start. A start from time.Now keeps
// its monotonic clock reading in every time of the Fake.
func NewFake(start time.Time) *Fake {
	return &Fake{now: start, changed: make(chan struct{})}
}

// Now returns the time of the Fake.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Since returns the time of the Fake since t.
func (f *Fake) Since(t time.Time) time.Duration {
	return f.Now().Sub(t)
}

// Until returns the time of the Fake until t.
func (f *Fake) Until(t time.Time) time.Duration {
	return t.Sub(f.Now())
}

// Sleep returns once Advance moved the time by d.
func (f *Fake) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	<-f.NewTimer(d).C()
}

// After returns the channel of a new timer of d.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	return f.NewTimer(d).C()
}

// NewTimer returns a timer that fires once the time moved by d.
func (f *Fake) NewTimer(d time.Duration) Timer {
	t := &fakeTimer{f: f, ch: make(chan time.Time, 1)}
	t.Reset(d)
	return t
}

// AfterFunc returns a timer that calls fn once the time moved by d. A
// timer due at once calls fn in its own goroutine, as time.AfterFunc does.
func (f *Fake) AfterFunc(d time.Duration, fn func()) Timer {
	t := &fakeTimer{f: f, fn: fn}
	t.Reset(d)
	return t
}

// NewTicker returns a ticker that ticks every d. A ticker of a d that is
// not positive never ticks.
func (f *Fake) NewTicker(d time.Duration) Ticker {
	t := &fakeTimer{f: f, ch: make(chan time.Time, 1)}
	t.resetTicker(d)
	return fakeTicker{t}
}

// Advance moves the time by d and fires every timer due by then.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	target := f.now.Add(d)
	for {
		t := f.next(target)
		if t == nil {
			break
		}
		if t.at.After(f.now) {
			f.now = t.at
		}
		if t.period > 0 {
			t.at = t.at.Add(t.period)
			t.seq = f.nextSeq()
		} else {
			f.disarm(t)
		}
		f.notify()
		now := f.now
		if t.fn != nil {
			f.mu.Unlock()
			t.fn()
			f.mu.Lock()
			continue
		}
		select {
		case t.ch <- now:
		default:
		}
	}
	if target.After(f.now) {
		f.now = target
	}
	f.mu.Unlock()
}

// Waiters returns how many timers and tickers wait to fire. A timer that
// fired waits again only once it is reset.
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.armed)
}

// BlockUntil returns once at least n timers and tickers wait to fire, or
// the error of ctx once it is done. A goroutine that waits on the Fake in
// a loop has parked again once it reset its timer.
func (f *Fake) BlockUntil(ctx context.Context, n int) error {
	for {
		f.mu.Lock()
		if len(f.armed) >= n {
			f.mu.Unlock()
			return nil
		}
		changed := f.changed
		f.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return fmt.Errorf("waiting for %d timers: %w", n, ctx.Err())
		}
	}
}

func (f *Fake) next(by time.Time) *fakeTimer {
	var first *fakeTimer
	for _, t := range f.armed {
		if t.at.After(by) {
			continue
		}
		if first == nil || t.at.Before(first.at) || (t.at.Equal(first.at) && t.seq < first.seq) {
			first = t
		}
	}
	return first
}

func (f *Fake) nextSeq() uint64 {
	f.seq++
	return f.seq
}

func (f *Fake) notify() {
	close(f.changed)
	f.changed = make(chan struct{})
}

func (f *Fake) arm(t *fakeTimer, at time.Time) {
	t.at = at
	t.seq = f.nextSeq()
	if !t.armed {
		t.armed = true
		f.armed = append(f.armed, t)
	}
	f.notify()
}

func (f *Fake) disarm(t *fakeTimer) bool {
	if !t.armed {
		return false
	}
	t.armed = false
	for i, a := range f.armed {
		if a == t {
			f.armed = append(f.armed[:i], f.armed[i+1:]...)
			break
		}
	}
	f.notify()
	return true
}

func (t *fakeTimer) C() <-chan time.Time {
	return t.ch
}

func (t *fakeTimer) drain() {
	if t.ch == nil {
		return
	}
	select {
	case <-t.ch:
	default:
	}
}

func (t *fakeTimer) Stop() bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	t.drain()
	return t.f.disarm(t)
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	f := t.f
	f.mu.Lock()
	t.drain()
	was := f.disarm(t)
	if d > 0 {
		f.arm(t, f.now.Add(d))
		f.mu.Unlock()
		return was
	}
	now := f.now
	f.mu.Unlock()
	if t.fn != nil {
		go t.fn()
		return was
	}
	select {
	case t.ch <- now:
	default:
	}
	return was
}

func (t *fakeTimer) resetTicker(d time.Duration) {
	f := t.f
	f.mu.Lock()
	defer f.mu.Unlock()
	t.drain()
	f.disarm(t)
	t.period = d
	if d > 0 {
		f.arm(t, f.now.Add(d))
	}
}

type fakeTicker struct{ t *fakeTimer }

func (k fakeTicker) C() <-chan time.Time   { return k.t.ch }
func (k fakeTicker) Stop()                 { k.t.Stop() }
func (k fakeTicker) Reset(d time.Duration) { k.t.resetTicker(d) }
