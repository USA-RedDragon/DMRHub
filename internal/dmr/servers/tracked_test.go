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

package servers_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrackedWaitsForItsGoroutines starts a goroutine with Go that runs
// until the test releases it. Wait must not return before it ends, and a
// Go after Wait began must run its function before Go returns.
func TestTrackedWaitsForItsGoroutines(t *testing.T) {
	t.Parallel()
	var tr servers.Tracked
	release := make(chan struct{})
	ended := make(chan struct{})
	tr.Go(func() {
		<-release
		close(ended)
	})
	waited := make(chan struct{})
	go func() {
		tr.Wait()
		close(waited)
	}()
	select {
	case <-waited:
		require.Fail(t, "Wait returned while a goroutine of Go ran")
	case <-ended:
		require.Fail(t, "the goroutine ended before the test released it")
	default:
	}
	close(release)
	<-waited
	select {
	case <-ended:
	default:
		require.Fail(t, "Wait returned before the goroutine of Go ended")
	}
	ran := false
	tr.Go(func() { ran = true })
	assert.True(t, ran, "a Go after Wait ran its function later")
}
