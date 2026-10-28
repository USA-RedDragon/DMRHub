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

package lookup_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDown = errors.New("database down")

func TestStaleAnswerComesAtOnceAndIsReplaced(t *testing.T) {
	t.Parallel()
	var answer atomic.Int64
	answer.Store(1)
	release := make(chan struct{})
	var slow atomic.Bool
	c := lookup.New(clock.Real(), "test", 20*time.Millisecond, func(int) (int64, error) {
		if slow.Load() {
			<-release
		}
		return answer.Load(), nil
	})

	v, err := c.Get(7)
	require.NoError(t, err)
	assert.Equal(t, int64(1), v)

	answer.Store(2)
	slow.Store(true)
	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	v, err = c.Get(7)
	require.NoError(t, err)
	assert.Equal(t, int64(1), v, "a stale answer comes while the load waits")
	assert.Less(t, time.Since(start), 10*time.Millisecond)
	close(release)
	assert.Eventually(t, func() bool {
		v, err := c.Get(7)
		return err == nil && v == 2
	}, time.Second, 5*time.Millisecond)
}

func TestFailedReloadKeepsTheAnswer(t *testing.T) {
	t.Parallel()
	var down atomic.Bool
	var loads atomic.Int64
	c := lookup.New(clock.Real(), "test", 10*time.Millisecond, func(int) (string, error) {
		loads.Add(1)
		if down.Load() {
			return "", errDown
		}
		return "up", nil
	})
	_, err := c.Get(1)
	require.NoError(t, err)

	down.Store(true)
	time.Sleep(20 * time.Millisecond)
	for range 50 {
		v, err := c.Get(1)
		require.NoError(t, err)
		assert.Equal(t, "up", v)
	}
	time.Sleep(5 * time.Millisecond)
	assert.LessOrEqual(t, loads.Load(), int64(3), "a failed reload waits a TTL before the next")
}

func TestColdLoadErrorIsNotKept(t *testing.T) {
	t.Parallel()
	var down atomic.Bool
	down.Store(true)
	c := lookup.New(clock.Real(), "test", time.Minute, func(int) (bool, error) {
		if down.Load() {
			return false, errDown
		}
		return true, nil
	})
	_, err := c.Get(1)
	require.ErrorIs(t, err, errDown)
	down.Store(false)
	v, err := c.Get(1)
	require.NoError(t, err)
	assert.True(t, v)
}

func TestDropAndPrune(t *testing.T) {
	t.Parallel()
	var loads atomic.Int64
	c := lookup.New(clock.Real(), "test", time.Minute, func(int) (int64, error) { return loads.Add(1), nil })
	v, _ := c.Get(1)
	assert.Equal(t, int64(1), v)
	c.Drop(1)
	v, _ = c.Get(1)
	assert.Equal(t, int64(2), v)
	c.Prune(time.Now().Add(lookup.Idle + time.Second))
	v, _ = c.Get(1)
	assert.Equal(t, int64(3), v)
}

func TestReloadReplacesAFreshAnswerAndSkipsUnknownKeys(t *testing.T) {
	t.Parallel()
	var loads atomic.Int64
	c := lookup.New(clock.Real(), "test", time.Minute, func(int) (int64, error) { return loads.Add(1), nil })
	c.Reload(1)
	assert.Zero(t, loads.Load(), "a key nobody read is not loaded")
	v, _ := c.Get(1)
	assert.Equal(t, int64(1), v)
	c.Reload(1)
	v, _ = c.Get(1)
	assert.Equal(t, int64(2), v, "a reload replaces an answer that is not stale yet")
}

func TestAStalledLoadHoldsReadsForWaitOnce(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var loads atomic.Int64
	c := lookup.New(clock.Real(), "test", time.Hour, func(int) (int, error) {
		loads.Add(1)
		<-release
		return 7, nil
	})
	start := time.Now()
	_, err := c.Get(1)
	require.ErrorIs(t, err, lookup.ErrSlow)
	took := time.Since(start)
	assert.GreaterOrEqual(t, took, lookup.Wait)
	assert.Less(t, took, lookup.Wait+time.Second)

	start = time.Now()
	_, err = c.Get(1)
	require.ErrorIs(t, err, lookup.ErrSlow)
	assert.Less(t, time.Since(start), 50*time.Millisecond, "a read waited again for a load that already used its wait")

	close(release)
	assert.Eventually(t, func() bool {
		v, err := c.Get(1)
		return err == nil && v == 7
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, int64(1), loads.Load(), "loads of one key")
}

func TestHeldWaitsEndWithTheLoadOrTheirEnd(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	c := lookup.New(clock.Real(), "test", time.Hour, func(k int) (int, error) {
		if k == 2 {
			<-release
		}
		return k, nil
	})
	end := make(chan struct{})
	c.HoldWaits(end)

	v, err := c.Get(1)
	require.NoError(t, err)
	assert.Equal(t, 1, v)

	got := make(chan error, 1)
	go func() {
		_, err := c.Get(2)
		got <- err
	}()
	select {
	case err := <-got:
		require.Fail(t, "a held wait ended before its end", "%v", err)
	case <-time.After(2 * lookup.Wait):
	}
	close(end)
	require.ErrorIs(t, <-got, lookup.ErrSlow)
	close(release)
	assert.Eventually(t, func() bool {
		v, err := c.Get(2)
		return err == nil && v == 2
	}, time.Minute, 5*time.Millisecond)
}
