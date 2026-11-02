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

package dmr_test

import (
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestRowChecksWaitForTheCallTracker holds the call tracker in the write
// of the first row of a call, as a starved database does, and checks the
// row only when flush lets the write go on. A row check must wait for the
// tracker to handle the packets that the hub got before it, and so find
// the row.
func TestRowChecksWaitForTheCallTracker(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		entered, release := make(chan struct{}), make(chan struct{})
		var enter, free sync.Once
		let := func() { free.Do(func() { close(release) }) }
		t.Cleanup(let)
		require.NoError(t, stack.DB.Callback().Create().Before("gorm:create").Register("hold_call_rows", func(tx *gorm.DB) {
			if tx.Statement.Table != "calls" {
				return
			}
			enter.Do(func() { close(entered) })
			<-release
		}))
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.StartServers(t)

		c := testutils.NewMMDVMClient(100001, "", testPassword)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		require.NoError(t, c.WaitReady(handshakeWait))
		require.NoError(t, c.SendDMRD(makeGroupVoicePacket(1000001, 1, 7301, false)))
		<-entered

		r := recordsOf(stack)
		flush := r.flush
		r.flush = func() {
			let()
			flush()
		}
		requireRows(t, r, "calls", "stream_id", 7301, map[string]any{"kind": "voice", "source_id": 1000001})
	})
}
