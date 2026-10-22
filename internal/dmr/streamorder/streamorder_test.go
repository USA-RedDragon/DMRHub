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

package streamorder_test

import (
	"context"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/streamorder"
	"github.com/stretchr/testify/assert"
)

func TestQueueKeepsOrderPerStream(t *testing.T) {
	t.Parallel()
	done := make(chan struct{})
	defer close(done)
	q := streamorder.New(4, 8, done)

	const streams, count = 6, 200
	var mu sync.Mutex
	got := map[uint][]int{}
	var wg sync.WaitGroup
	for i := range count {
		for s := range uint(streams) {
			wg.Add(1)
			assert.True(t, q.Do(context.Background(), s, func() {
				defer wg.Done()
				mu.Lock()
				got[s] = append(got[s], i)
				mu.Unlock()
			}))
		}
	}
	wg.Wait()
	want := make([]int, count)
	for i := range want {
		want[i] = i
	}
	for s := range uint(streams) {
		assert.Equal(t, want, got[s], "stream %d", s)
	}
}

func TestQueueRefusesAfterStop(t *testing.T) {
	t.Parallel()
	done := make(chan struct{})
	q := streamorder.New(1, 1, done)
	close(done)
	assert.False(t, q.Do(context.Background(), 1, func() { t.Error("ran after stop") }))

	liveDone := make(chan struct{})
	defer close(liveDone)
	live := streamorder.New(1, 1, liveDone)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, live.Do(ctx, 1, func() { t.Error("ran after cancel") }))
}
