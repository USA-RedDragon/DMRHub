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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAFullQueueKeepsTheJobsOfASession fills the queue of a repeater with
// jobs that a held store keeps waiting. Do must drop a job then, but Must
// must queue its job behind them until MaxStateJobsPerRepeater wait, and
// Always must queue its job past that, all in order.
func TestAFullQueueKeepsTheJobsOfASession(t *testing.T) {
	t.Parallel()
	var (
		q       RepeaterQueue
		mu      sync.Mutex
		order   []int
		release = make(chan struct{})
		running = make(chan struct{})
	)
	note := func(i int) func() {
		return func() {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, i)
		}
	}
	require.True(t, q.Do(1, func() {
		close(running)
		<-release
	}))
	<-running
	for i := range maxQueuedPerRepeater {
		require.True(t, q.Do(1, note(i)))
	}
	assert.False(t, q.Do(1, note(-1)))
	for i := maxQueuedPerRepeater; i < MaxStateJobsPerRepeater; i++ {
		require.True(t, q.Must(1, note(i)))
	}
	assert.False(t, q.Must(1, note(-1)))
	q.Always(1, note(MaxStateJobsPerRepeater))
	close(release)
	q.Wait()
	require.Len(t, order, MaxStateJobsPerRepeater+1)
	for i, got := range order {
		require.Equal(t, i, got)
	}
}
