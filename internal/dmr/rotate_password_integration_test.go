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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRotatedPasswordEndsTheSessionOnEveryReplica rotates the password of a
// hotspot that is logged in to the second replica through the hub of the
// first: the second replica must end the session with MSTCL at once, refuse
// a login with the old password, and take one with the new password.
func TestRotatedPasswordEndsTheSessionOnEveryReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const id = 100001
		const newPassword = "n3wPassw0rd9"
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		live := testutils.NewMMDVMClient(id, "", testPassword)
		require.NoError(t, live.Connect(second.MMDVMAddr))
		defer live.Close()
		require.NoError(t, live.WaitReady(handshakeWait))
		require.True(t, live.Ping(handshakeWait))

		require.NoError(t, models.SetRepeaterPassword(stack.DB, id, newPassword))
		stack.Hub.ReloadRepeater(t.Context(), id)
		stack.Hub.RepeaterSecretChanged(id)

		require.Eventually(t, func() bool { return live.MSTCLs() == 1 }, 2*time.Second, 10*time.Millisecond,
			"the replica that holds the session must end it with MSTCL")
		require.False(t, live.Ping(time.Second), "the ended session must not answer pings")

		old := testutils.NewMMDVMClient(id, "", testPassword)
		require.NoError(t, old.Connect(second.MMDVMAddr))
		defer old.Close()
		require.Error(t, old.WaitReady(3*time.Second), "a login with the old password must fail")

		fresh := testutils.NewMMDVMClient(id, "", newPassword)
		require.NoError(t, fresh.Connect(second.MMDVMAddr))
		defer fresh.Close()
		require.NoError(t, fresh.WaitReady(handshakeWait), "a login with the new password must work")
	})
}

// TestRotatedAuthKeyStopsAtOnceOnEveryReplica rotates the auth key of an
// IPSC repeater that is registered with the second replica through the hub
// of the first: from then on neither side may take a packet signed with
// the old key, the replica that holds the repeater must drop it at once and
// send it nothing more, and the repeater works again once it registers
// anew with the new key.
func TestRotatedAuthKeyStopsAtOnceOnEveryReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const newKey = "00000000000000000000000000000000000a1b2c"
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 200001, 1)
		stack.AssignTS1StaticTG(t, 100001, 1)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, mc.Connect(second.MMDVMAddr))
		defer mc.Close()
		require.NoError(t, mc.WaitReady(handshakeWait))
		old := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, old.Connect(second.IPSCAddr))
		defer old.Close()
		require.NoError(t, old.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		call := func(stream uint) {
			require.NoError(t, mc.SendDMRD(makeGroupVoicePacket(1000001, 1, stream, false)))
			time.Sleep(callHoldTime)
			require.NoError(t, mc.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, stream, false)))
		}
		call(301)
		require.NotEmpty(t, old.Drain(drainWait), "the repeater must get the call before the rotation")
		before, err := models.FindRepeaterByID(stack.DB, 200001)
		require.NoError(t, err)

		require.NoError(t, models.SetRepeaterPassword(stack.DB, 200001, newKey))
		stack.Hub.ReloadRepeater(t.Context(), 200001)
		stack.Hub.RepeaterSecretChanged(200001)
		time.Sleep(settleDuration)

		heard := old.Heard()
		call(302)
		assert.Empty(t, old.Drain(drainWait), "a repeater with the old key must not take the packets of DMRHub")
		assert.Equal(t, heard, old.Heard(), "the replica must drop the repeater at once and send it nothing more")
		require.NoError(t, old.SendGroupVoice(1000001, 1, false, 303))
		assert.Empty(t, mc.Drain(drainWait), "DMRHub must not take a packet signed with the old key")

		fresh := testutils.NewIPSCClient(200001, newKey)
		require.NoError(t, fresh.Connect(second.IPSCAddr))
		defer fresh.Close()
		require.NoError(t, fresh.WaitReady(handshakeWait), "the repeater must register with the new key")
		assert.Eventually(t, func() bool {
			r, err := models.FindRepeaterByID(stack.DB, 200001)
			return err == nil && r.Connected.After(before.Connected)
		}, 2*time.Second, 10*time.Millisecond, "the repeater must register anew, not stay registered")
		time.Sleep(settleDuration)
		call(304)
		assert.NotEmpty(t, fresh.Drain(drainWait), "the repeater must get calls with the new key")
	})
}
