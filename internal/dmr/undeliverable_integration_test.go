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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBurstsIPSCCannotCarryAreReported sends a rate 1 data burst to a
// talkgroup that an IPSC peer carries. IPSC has no layout for it, so the hub
// must report it undeliverable to that peer, and the MMDVM repeater on the
// talkgroup must still get it.
func TestBurstsIPSCCannotCarryAreReported(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.AssignTS1StaticTG(t, 200001, 1)

		var mu sync.Mutex
		var got []models.Packet
		var targets []string
		stack.Hub.SetUndeliverableFunc(func(p models.Packet, target, _ string) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, p)
			targets = append(targets, target)
		})
		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		defer c1.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c2.Close()
		require.NoError(t, c2.WaitReady(handshakeWait))
		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, ic.Connect(stack.IPSCAddr))
		defer ic.Close()
		require.NoError(t, ic.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		require.NoError(t, c1.SendDMRD(makeBurst(1000001, 1, 6101, true, dmrconst.FrameDataSync, uint(enums.DataTypeRate1), 0)))
		assert.Len(t, c2.Drain(drainWait), 1, "the MMDVM repeater must get the rate 1 burst")
		assert.Empty(t, ic.Drain(100*time.Millisecond), "the IPSC peer must not get a burst it cannot carry")

		mu.Lock()
		defer mu.Unlock()
		require.Len(t, got, 1, "the burst must be reported undeliverable once")
		assert.Equal(t, models.RepeaterTypeIPSC, targets[0])
		assert.Equal(t, uint(200001), got[0].Repeater)
		assert.Equal(t, uint(6101), got[0].StreamID)
	})
}
