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

package testutils_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestCollectReturnsWhenDoneAccepts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ch := make(chan int, 4)
		ch <- 1
		ch <- 2
		ch <- 3
		start := time.Now()
		got := testutils.Collect(ch, time.Minute, func(v []int) bool { return len(v) == 2 })
		assert.Equal(t, []int{1, 2}, got)
		assert.Zero(t, time.Since(start), "Collect waited after done accepted")
		assert.Len(t, ch, 1, "Collect read past what done accepted")
	})
}

func TestCollectReadsTheWholeWindowWithoutDone(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ch := make(chan int)
		go func() {
			ch <- 1
			time.Sleep(59 * time.Second)
			ch <- 2
		}()
		start := time.Now()
		got := testutils.Collect(ch, time.Minute, nil)
		assert.Equal(t, []int{1, 2}, got)
		assert.Equal(t, time.Minute, time.Since(start))
	})
}
