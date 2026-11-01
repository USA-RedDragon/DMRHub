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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// TestIPSCSlowDatabaseHoldsNoBurst sends an IPSC call while the database
// holds every statement, after a call from the same peer ran once. The
// call must reach the MMDVM repeater while the database holds a statement
// of it, and the statement must run when the database lets it go.
func TestIPSCSlowDatabaseHoldsNoBurst(t *testing.T) {
	t.Parallel()
	forAllBackendsWith(t, []testutils.Backend{testutils.BoundedWaits()}, func(t *testing.T, stack *testutils.IntegrationStack) {
		db := holdDatabase(t, stack.DB)
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		t.Cleanup(mc.Close)
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		t.Cleanup(ic.Close)
		require.NoError(t, ic.Connect(stack.IPSCAddr))
		require.NoError(t, ic.WaitReady(handshakeWait))

		fromCaller := func(got []models.Packet) (n int, ended bool) {
			for _, p := range got {
				if p.Src == 1000001 && p.Dst == 1 {
					n++
					ended = ended || p.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(p.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm
				}
			}
			return n, ended
		}
		call := func(stream uint32) {
			require.NoError(t, ic.SendGroupVoice(1000001, 1, false, stream))
			got := testutils.Collect(mc.ReceivedPackets(), deliverWait, func(got []models.Packet) bool {
				n, ended := fromCaller(got)
				return n >= 3 && ended
			})
			n, ended := fromCaller(got)
			require.GreaterOrEqual(t, n, 3, "stream %d: the call did not reach the repeater", stream)
			require.True(t, ended, "stream %d: the terminator did not reach the repeater", stream)
		}

		call(12001)
		stack.CallTracker.Flush()
		require.Positive(t, db.ranOn("delete")+db.ranOn("update"), "the call tracker ends the row of the first call at its terminator")

		db.hold()
		call(12002)
		waitFor(t, "the call runs a statement for the database to hold", func() bool { return db.held.Load() > 0 })
		t.Logf("the call passed while the database held %d statements", db.held.Load())
		db.release()
		waitFor(t, "the held statements run when the database lets them go", func() bool { return db.held.Load() == 0 })
	})
}
