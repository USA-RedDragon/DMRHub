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
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func burstOf(stream uint, n int) models.Packet {
	p := makeGroupVoicePacket(1000001, 1, stream, false)
	p.Seq = uint(n)
	return p
}

func sendUntil(t *testing.T, talker, kept *testutils.MMDVMClient, stream uint, from int, stop <-chan struct{}) <-chan int {
	t.Helper()
	sent := make(chan int, 1)
	go func() {
		n := from
		defer func() { sent <- n }()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if talker.SendDMRD(burstOf(stream, n)) != nil {
				return
			}
			n++
			if len(testutils.Collect(kept.ReceivedPackets(), deliverWait, gotN[models.Packet](1))) == 0 {
				return
			}
		}
	}()
	return sent
}

// TestADroppedCallStopsOnItsRepeaterOnEveryReplica drops a TG call on one
// listening hotspot from the hub of the other replica, and then a call at
// the hotspot it comes from. A dropped hotspot gets the terminator of the
// call and then nothing of it, while the other hotspot keeps the call.
// The next call reaches the dropped hotspot again. A drop at the source
// ends the call for every listener with a terminator.
func TestADroppedCallStopsOnItsRepeaterOnEveryReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		for _, id := range []uint{100001, 100002, 100003} {
			stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
		}
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.AssignTS1StaticTG(t, 100003, 1)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		connect := func(id uint32, addr string) *testutils.MMDVMClient {
			c := testutils.NewMMDVMClient(id, "", testPassword)
			require.NoError(t, c.Connect(addr))
			t.Cleanup(c.Close)
			require.NoError(t, c.WaitReady(handshakeWait))
			return c
		}
		talker := connect(100001, stack.MMDVMAddr)
		dropped := connect(100002, second.MMDVMAddr)
		kept := connect(100003, stack.MMDVMAddr)
		waitSubscribed(t, second.Hub, 100002, 1, true)
		waitSubscribed(t, stack.Hub, 100003, 1, true)

		require.NoError(t, talker.SendDMRD(burstOf(7701, 1)))
		require.NotEmpty(t, testutils.Collect(kept.ReceivedPackets(), deliverWait, gotN[models.Packet](1)))
		require.NotEmpty(t, testutils.Collect(dropped.ReceivedPackets(), deliverWait, gotN[models.Packet](1)))

		stack.Hub.DropCall(100002, false, 7701, 1000001, 1)
		stop := make(chan struct{})
		sent := sendUntil(t, talker, kept, 7701, 2, stop)
		got := testutils.Collect(dropped.ReceivedPackets(), deliverWait, callEnds(1))
		close(stop)
		n := <-sent
		require.NotEmpty(t, got)
		require.True(t, isTerminator(got[len(got)-1]), "the dropped hotspot gets the terminator of the call")

		for i := range 3 {
			require.NoError(t, talker.SendDMRD(burstOf(7701, n+i)))
			require.NotEmpty(t, testutils.Collect(kept.ReceivedPackets(), deliverWait, gotN[models.Packet](1)), "the other hotspot keeps the call")
		}
		require.NoError(t, talker.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7701, false)))
		require.NotEmpty(t, receiveCall(kept, 1))
		assert.Empty(t, dropped.Drain(drainWait), "the dropped hotspot got more of the call")

		sendGroupCall(t, talker.SendDMRD, 1, 7702, false)
		assert.NotEmpty(t, receiveCall(dropped, 1), "the next call reaches the hotspot again")
		assert.NotEmpty(t, receiveCall(kept, 1))

		require.NoError(t, talker.SendDMRD(burstOf(7703, 1)))
		require.NotEmpty(t, testutils.Collect(kept.ReceivedPackets(), deliverWait, gotN[models.Packet](1)))
		require.NotEmpty(t, testutils.Collect(dropped.ReceivedPackets(), deliverWait, gotN[models.Packet](1)))
		stack.Hub.DropCall(100001, false, 7703, 1000001, 1)
		require.NoError(t, talker.SendDMRD(burstOf(7703, 2)))
		require.True(t, isTerminator(receiveCall(kept, 1)[0]), "the next burst of a call dropped at its source is a terminator")
		require.True(t, isTerminator(receiveCall(dropped, 1)[0]))
		require.NoError(t, talker.SendDMRD(burstOf(7703, 3)))
		assert.Empty(t, kept.Drain(drainWait), "a listener got more of a call dropped at its source")
	})
}

// TestAResetEndsTheSessionOnEveryReplica resets a hotspot that is logged
// in to the second replica through the hub of the first: the second
// replica ends the session with MSTCL, and the hotspot logs in again with
// its password.
func TestAResetEndsTheSessionOnEveryReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const id = 100001
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		live := testutils.NewMMDVMClient(id, "", testPassword)
		require.NoError(t, live.Connect(second.MMDVMAddr))
		defer live.Close()
		require.NoError(t, live.WaitReady(handshakeWait))

		stack.Hub.RepeaterReset(id)
		require.Len(t, testutils.Collect(live.MSTCLEvents(), deliverWait, gotN[struct{}](1)), 1, "the replica that holds the session must end it with MSTCL")
		require.Equal(t, uint64(1), live.MSTCLs())

		again := testutils.NewMMDVMClient(id, "", testPassword)
		require.NoError(t, again.Connect(second.MMDVMAddr))
		defer again.Close()
		require.NoError(t, again.WaitReady(handshakeWait), "the hotspot logs in again with its password")
	})
}
