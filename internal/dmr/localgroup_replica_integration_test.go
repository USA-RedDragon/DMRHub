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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type localReplicas struct {
	stack   *testutils.IntegrationStack
	second  testutils.Replica
	peer    *testutils.OpenBridgeClient
	on1     *testutils.MMDVMClient
	on2     *testutils.MMDVMClient
	network *testutils.MMDVMClient
	group   uint
	hubs    []*hub.Hub
}

// startLocalReplicas runs two replicas with TG 8 local and the local group
// North Texas on it: 100001 on TS2 on the first replica and 100003 on TS1
// on the second. 100004, on the second replica, carries the network TG 8
// and TG 1 on TS2, and an OpenBridge peer takes the calls of 1000001.
func startLocalReplicas(t *testing.T, stack *testutils.IntegrationStack) *localReplicas {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedTalkgroup(t, 8, "Network 8")
	for _, id := range []uint{100001, 100003, 100004} {
		stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
	}
	stack.AssignTS2StaticTG(t, 100004, 8)
	stack.AssignTS2StaticTG(t, 100004, 1)
	r := &localReplicas{stack: stack, second: stack.SpawnSecondReplica(t)}
	r.hubs = []*hub.Hub{stack.Hub, r.second.Hub}
	cfg := stack.Config()
	cfg.DMR.Routing = config.Routing{LocalTalkgroups: []uint{8, 9}, DisableDynamicLinks: true}
	for _, h := range r.hubs {
		h.SetConfig(cfg)
	}
	stack.StartServers(t)

	r.peer = testutils.NewOpenBridgeClient(500002, obPassword(t))
	t.Cleanup(r.peer.Close)
	require.NoError(t, r.peer.Connect(stack.OpenBridgeAddr))
	addr := r.peer.LocalAddr()
	stack.SeedPeer(t, 500002, 1000001, obPassword(t), addr.IP.String(), addr.Port, true, true)
	stack.SeedPeerRule(t, 500002, false, 1000001, 1000001)

	g := models.LocalGroup{Name: "North Texas", Talkgroup: 8, OwnerID: 1000001}
	require.NoError(t, stack.DB.Create(&g).Error)
	r.group = g.ID
	for _, m := range []models.LocalGroupMember{
		{LocalGroupID: g.ID, RepeaterID: 100001, Slot: 2, State: models.LocalMemberIn},
		{LocalGroupID: g.ID, RepeaterID: 100003, Slot: 1, State: models.LocalMemberIn},
	} {
		require.NoError(t, models.AddLocalGroupMember(stack.DB, m))
		stack.Hub.ReloadRepeater(t.Context(), m.RepeaterID)
	}

	r.on1 = r.client(t, 100001, stack.MMDVMAddr)
	r.on2 = r.client(t, 100003, r.second.MMDVMAddr)
	r.network = r.client(t, 100004, r.second.MMDVMAddr)
	waitFor(t, "repeater 100003 takes the calls of the group on the second replica", func() bool {
		return r.second.Hub.SubscribesLocal(100003, r.group)
	})
	waitFor(t, "repeater 100001 takes the calls of the group on the first replica", func() bool {
		return stack.Hub.SubscribesLocal(100001, r.group)
	})
	takeNotice(t, stack.PubSub, r.second.Hub.OnPeerChange, "hub:peers:changed")
	return r
}

func (r *localReplicas) client(t *testing.T, id uint32, at string) *testutils.MMDVMClient {
	t.Helper()
	c := testutils.NewMMDVMClient(id, "", testPassword)
	t.Cleanup(c.Close)
	require.NoError(t, c.Connect(at))
	require.NoError(t, c.WaitReady(handshakeWait))
	return c
}

// sentinel sends a call to TG 1 from on, on the slot of a local call before
// it, and returns what the network repeater and the peer got until they
// got it. They get the bursts of a source slot in the order they came, so
// what they did not get of the local call by then, they never get.
func (r *localReplicas) sentinel(t *testing.T, on *testutils.MMDVMClient, stream uint, slot bool) ([]models.Packet, []testutils.ReceivedBridge) {
	t.Helper()
	sendGroupCall(t, on.SendDMRD, 1, stream, slot)
	network := testutils.Collect(r.network.ReceivedPackets(), deliverWait, func(got []models.Packet) bool {
		return got[len(got)-1].StreamID == stream && isTerminator(got[len(got)-1])
	})
	bridges := r.peer.ReceiveBridges(deliverWait, func(b []testutils.ReceivedBridge) bool { return len(dataOf(b, uint32(stream))) >= 2 })
	return network, bridges
}

func streamOf(pkts []models.Packet, stream uint) []models.Packet {
	var out []models.Packet
	for _, p := range pkts {
		if p.StreamID == stream {
			out = append(out, p)
		}
	}
	return out
}

// waitLocalCall waits for the row of the call stream to the local group,
// which the call tracker makes at the first burst, and returns it.
func (r *localReplicas) waitLocalCall(t *testing.T, stream uint) models.Call {
	t.Helper()
	var call models.Call
	waitFor(t, "the call to the local group is recorded", func() bool {
		return r.stack.DB.Where("stream_id = ? AND is_to_local_group = ?", stream, true).First(&call).Error == nil
	})
	return call
}

// TestALocalGroupCallCrossesReplicas sends a call to the local TG 8 from a
// repeater of the group on each replica. The member on the other replica
// gets it on its own slot. The network TG 8 and the OpenBridge peer get
// nothing, and Last Heard records the call to the group.
func TestALocalGroupCallCrossesReplicas(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startLocalReplicas(t, stack)

		require.NoError(t, r.on1.SendDMRD(makeGroupVoicePacket(1000001, 8, 8101, true)))
		call := r.waitLocalCall(t, 8101)
		assert.Equal(t, "North Texas", call.LocalGroupName)
		require.NotNil(t, call.LocalGroupID)
		assert.Equal(t, r.group, *call.LocalGroupID)
		require.NoError(t, r.on1.SendDMRD(makeGroupVoiceTermPacket(1000001, 8, 8101, true)))
		got := streamOf(receiveCall(r.on2, 8), 8101)
		require.NotEmpty(t, got, "the member on the other replica gets the call")
		for _, p := range got {
			assert.False(t, p.Slot, "on TS1, the slot of 100003 in the group")
			assert.Equal(t, uint(100003), p.Repeater)
		}
		network, bridges := r.sentinel(t, r.on1, 8102, true)
		assert.Empty(t, streamOf(network, 8101), "the network TG 8 does not get the local call")
		assert.Empty(t, dataOf(bridges, 8101), "the OpenBridge peer does not get the local call")

		require.NoError(t, r.on2.SendDMRD(makeGroupVoicePacket(1000001, 8, 8103, false)))
		r.waitLocalCall(t, 8103)
		require.NoError(t, r.on2.SendDMRD(makeGroupVoiceTermPacket(1000001, 8, 8103, false)))
		back := streamOf(receiveCall(r.on1, 8), 8103)
		require.NotEmpty(t, back, "a call from the second replica reaches the member on the first")
		for _, p := range back {
			assert.True(t, p.Slot, "on TS2, the slot of 100001 in the group")
		}
		network, bridges = r.sentinel(t, r.on2, 8104, false)
		assert.Empty(t, streamOf(network, 8103))
		assert.Empty(t, dataOf(bridges, 8103))

		require.NoError(t, r.on1.SendDMRD(makeGroupVoicePacket(1000001, 8, 8105, false)))
		var off models.Call
		waitFor(t, "the call to TG 8 off the slot of the group is recorded", func() bool {
			return r.stack.DB.Where("stream_id = ?", 8105).First(&off).Error == nil
		})
		assert.False(t, off.IsToLocalGroup, "a call off the slot of the group is to no local group")
		assert.False(t, off.IsToTalkgroup, "nor to the network TG 8, which it did not reach")
		require.NoError(t, r.on1.SendDMRD(makeGroupVoiceTermPacket(1000001, 8, 8105, false)))
	})
}

// TestALocalGroupFollowsARepeaterThatMoves moves 100003 from the second
// replica to the first, as a load balancer does when a replica drains. The
// group call reaches it on the replica it moved to.
func TestALocalGroupFollowsARepeaterThatMoves(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startLocalReplicas(t, stack)

		r.on2.Close()
		moved := r.client(t, 100003, stack.MMDVMAddr)
		waitFor(t, "repeater 100003 takes the calls of the group on the first replica", func() bool {
			return stack.Hub.SubscribesLocal(100003, r.group)
		})

		sendGroupCall(t, r.on1.SendDMRD, 8, 8201, true)
		got := streamOf(receiveCall(moved, 8), 8201)
		require.NotEmpty(t, got, "the repeater that moved gets the call")
		for _, p := range got {
			assert.False(t, p.Slot)
		}
	})
}
