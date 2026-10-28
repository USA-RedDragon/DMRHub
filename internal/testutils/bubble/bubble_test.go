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

package bubble_test

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/assert"
)

func TestEventuallyWaitsInVirtualTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var done atomic.Bool
		time.AfterFunc(time.Hour, func() { done.Store(true) })
		start := time.Now()
		bubble.Eventually(t, done.Load, 2*time.Hour, time.Minute)
		took := time.Since(start)
		assert.GreaterOrEqual(t, took, time.Hour)
		assert.LessOrEqual(t, took, time.Hour+time.Minute)
	})
}

func TestReceiveReturnsTheValue(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ch := make(chan int, 1)
		time.AfterFunc(time.Minute, func() { ch <- 7 })
		assert.Equal(t, 7, bubble.Receive(t, ch, time.Hour))
	})
}
