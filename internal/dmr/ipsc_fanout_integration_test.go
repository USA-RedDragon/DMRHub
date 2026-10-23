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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIPSCPeersGetOnlyTheirTraffic connects two IPSC peers. Only the one
// that carries TG1 may get a TG1 call, once, and a private call to a
// repeater ID may reach only that repeater.
func TestIPSCPeersGetOnlyTheirTraffic(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.SeedIPSCRepeater(t, 200002, 1000001, ipscAuthKey)
		stack.AssignTS1StaticTG(t, 200001, 1)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		defer mc.Close()
		require.NoError(t, mc.WaitReady(handshakeWait))
		ic1 := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, ic1.Connect(stack.IPSCAddr))
		defer ic1.Close()
		require.NoError(t, ic1.WaitReady(handshakeWait))
		ic2 := testutils.NewIPSCClient(200002, ipscAuthKey)
		require.NoError(t, ic2.Connect(stack.IPSCAddr))
		defer ic2.Close()
		require.NoError(t, ic2.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		require.NoError(t, mc.SendDMRD(makeGroupVoicePacket(1000001, 1, 5101, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, mc.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 5101, false)))
		got1 := ic1.Drain(drainWait)
		got2 := ic2.Drain(100 * time.Millisecond)
		assert.Len(t, got1, 2, "the IPSC peer on TG1 must get each burst of the call once")
		assert.Empty(t, got2, "an IPSC peer without TG1 must not get a TG1 call")

		priv := makeGroupVoicePacket(1000001, 200002, 5102, false)
		priv.GroupCall = false
		privTerm := makeGroupVoiceTermPacket(1000001, 200002, 5102, false)
		privTerm.GroupCall = false
		require.NoError(t, mc.SendDMRD(priv))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, mc.SendDMRD(privTerm))
		got2 = ic2.Drain(drainWait)
		got1 = ic1.Drain(100 * time.Millisecond)
		assert.NotEmpty(t, got2, "the target IPSC repeater must get its private call")
		assert.Empty(t, got1, "another IPSC peer must not get a private call to 200002")
	})
}
