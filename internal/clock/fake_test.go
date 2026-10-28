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

package clock_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var start = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals

func fired(c <-chan time.Time) (time.Time, bool) {
	select {
	case at := <-c:
		return at, true
	default:
		return time.Time{}, false
	}
}

func TestFakeTimeMovesOnlyOnAdvance(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	assert.Equal(t, start, f.Now())
	f.Advance(3 * time.Second)
	assert.Equal(t, start.Add(3*time.Second), f.Now())
	assert.Equal(t, 3*time.Second, f.Since(start))
	assert.Equal(t, 2*time.Second, f.Until(start.Add(5*time.Second)))
}

func TestFakeTimerFiresAtItsTime(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	tm := f.NewTimer(time.Second)
	assert.Equal(t, 1, f.Waiters())
	f.Advance(999 * time.Millisecond)
	_, ok := fired(tm.C())
	assert.False(t, ok, "a timer fires no sooner than its time")
	f.Advance(time.Millisecond)
	at, ok := fired(tm.C())
	require.True(t, ok)
	assert.Equal(t, start.Add(time.Second), at)
	assert.Equal(t, 0, f.Waiters())
}

func TestFakeStopAndResetDropTheStaleValue(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	tm := f.NewTimer(time.Second)
	f.Advance(time.Second)
	assert.False(t, tm.Stop(), "a fired timer was not armed")
	_, ok := fired(tm.C())
	assert.False(t, ok, "Stop drops the value that nobody took")
	assert.False(t, tm.Reset(time.Second))
	assert.True(t, tm.Stop())
	f.Advance(time.Hour)
	_, ok = fired(tm.C())
	assert.False(t, ok, "a stopped timer does not fire")
}

func TestFakeFiresInTimeOrder(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	var got []time.Duration
	for _, d := range []time.Duration{3 * time.Second, time.Second, 2 * time.Second, time.Second} {
		f.AfterFunc(d, func() { got = append(got, f.Since(start)) })
	}
	f.Advance(10 * time.Second)
	assert.Equal(t, []time.Duration{time.Second, time.Second, 2 * time.Second, 3 * time.Second}, got)
	assert.Equal(t, start.Add(10*time.Second), f.Now())
}

func TestFakeAfterFuncCanRearm(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	var n int
	var tm clock.Timer
	tm = f.AfterFunc(time.Second, func() {
		n++
		tm.Reset(time.Second)
	})
	f.Advance(5 * time.Second)
	assert.Equal(t, 5, n)
}

func TestFakeTickerTicksAndDropsUntakenTicks(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	k := f.NewTicker(time.Second)
	f.Advance(time.Second)
	at, ok := fired(k.C())
	require.True(t, ok)
	assert.Equal(t, start.Add(time.Second), at)
	f.Advance(5 * time.Second)
	at, ok = fired(k.C())
	require.True(t, ok)
	assert.Equal(t, start.Add(2*time.Second), at, "the first untaken tick stays and the rest drop")
	_, ok = fired(k.C())
	assert.False(t, ok)
	k.Stop()
	assert.Equal(t, 0, f.Waiters())
}

func TestFakeDueTimerFiresAtOnce(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	_, ok := fired(f.NewTimer(0).C())
	assert.True(t, ok)
	done := make(chan struct{})
	f.AfterFunc(-time.Second, func() { close(done) })
	<-done
}

func TestFakeBlockUntilWaitsForALoopToPark(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	var mu sync.Mutex
	var runs int
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		tm := f.NewTimer(time.Minute)
		for {
			select {
			case <-tm.C():
				mu.Lock()
				runs++
				mu.Unlock()
				tm.Reset(time.Minute)
			case <-ctx.Done():
				return
			}
		}
	}()
	for i := 1; i <= 3; i++ {
		require.NoError(t, f.BlockUntil(ctx, 1))
		f.Advance(time.Minute)
		require.NoError(t, f.BlockUntil(ctx, 1))
		mu.Lock()
		assert.Equal(t, i, runs)
		mu.Unlock()
	}
}

func TestFakeBlockUntilEndsWithTheContext(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, f.BlockUntil(ctx, 1), context.Canceled)
}

func TestFakeSleepReturnsOnAdvance(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	done := make(chan struct{})
	go func() {
		f.Sleep(time.Hour)
		close(done)
	}()
	require.NoError(t, f.BlockUntil(context.Background(), 1))
	f.Advance(time.Hour)
	<-done
	f.Sleep(0)
}

func TestRealIsTheTimePackage(t *testing.T) {
	t.Parallel()
	c := clock.Or(nil)
	before := time.Now()
	assert.False(t, c.Now().Before(before))
	tm := c.NewTimer(time.Hour)
	assert.True(t, tm.Stop())
	k := c.NewTicker(time.Hour)
	k.Stop()
	f := c.AfterFunc(time.Hour, func() {})
	assert.True(t, f.Stop())
	assert.GreaterOrEqual(t, c.Since(before), time.Duration(0))
}

func TestWithTimeoutEndsOnTheFakeTime(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	ctx, cancel := clock.WithTimeout(context.Background(), f, time.Second)
	defer cancel()
	child, stop := context.WithCancel(ctx)
	defer stop()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	assert.Equal(t, start.Add(time.Second), deadline)
	f.Advance(999 * time.Millisecond)
	require.NoError(t, ctx.Err())
	f.Advance(time.Millisecond)
	<-child.Done()
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.ErrorIs(t, child.Err(), context.DeadlineExceeded)
}

func TestWithTimeoutEndsWithItsParent(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := clock.WithTimeout(parent, f, time.Hour)
	defer cancel()
	cancelParent()
	<-ctx.Done()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestWithTimeoutCancel(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(start)
	ctx, cancel := clock.WithTimeout(context.Background(), f, time.Second)
	cancel()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.Equal(t, 0, f.Waiters())
	onReal, cancelReal := clock.WithTimeout(context.Background(), clock.Real(), time.Hour)
	defer cancelReal()
	_, ok := onReal.Deadline()
	assert.True(t, ok)
}
