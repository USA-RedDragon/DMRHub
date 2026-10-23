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
	"context"
	"slices"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func staticIDs(t *testing.T, stack *testutils.IntegrationStack, id uint) (ts1, ts2 []uint) {
	t.Helper()
	rpt, err := models.FindRepeaterByID(stack.DB, id)
	require.NoError(t, err)
	for _, tg := range rpt.TS1StaticTalkgroups {
		ts1 = append(ts1, tg.ID)
	}
	for _, tg := range rpt.TS2StaticTalkgroups {
		ts2 = append(ts2, tg.ID)
	}
	slices.Sort(ts1)
	slices.Sort(ts2)
	return ts1, ts2
}

func waitStatics(t *testing.T, stack *testutils.IntegrationStack, id uint, ts1, ts2 []uint) {
	t.Helper()
	require.Eventually(t, func() bool {
		got1, got2 := staticIDs(t, stack, id)
		return slices.Equal(got1, ts1) && slices.Equal(got2, ts2)
	}, handshakeWait, 20*time.Millisecond, "static talkgroups of %d should become TS1 %v TS2 %v", id, ts1, ts2)
}

func sendGroupCall(t *testing.T, send func(models.Packet) error, dst, stream uint, slot bool) {
	t.Helper()
	require.NoError(t, send(makeGroupVoicePacket(1000001, dst, stream, slot)))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, send(makeGroupVoiceTermPacket(1000001, dst, stream, slot)))
}

// TestRPTOStaticTalkgroupsRoute sends FreeDMR options that replace the
// static talkgroups that the owner set, and checks that calls follow the
// new ones at once.
func TestRPTOStaticTalkgroupsRoute(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedTalkgroup(t, 10, "TG10")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))

		require.NoError(t, c2.SendOptions("TS1=;TS2=10;DIAL=0;VOICE=1;TIMER=10;"))
		waitStatics(t, stack, 100002, nil, []uint{10})
		time.Sleep(settleDuration)

		sendGroupCall(t, c1.SendDMRD, 10, 9101, true)
		pkts := c2.Drain(drainWait)
		require.NotEmpty(t, pkts, "the repeater should hear the TS2 static talkgroup that its options set")
		for _, p := range pkts {
			assert.Equal(t, uint(10), p.Dst)
			assert.True(t, p.Slot, "the call should be on TS2")
		}

		time.Sleep(callHoldTime)
		sendGroupCall(t, c1.SendDMRD, 1, 9102, false)
		for _, p := range c2.Drain(drainWait) {
			assert.NotEqual(t, uint(1), p.Dst, "the options cleared the TS1 static talkgroup")
		}
	})
}

// TestRPTOAcrossReplicas logs a DMRGateway-like peer with DMR+ options in
// to the second replica and calls it from a repeater on the first. Then the
// second replica stops as in a rolling restart and the peer moves to the
// first, which must route the talkgroups that the options saved.
func TestRPTOAcrossReplicas(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 10, "TG10")
		stack.SeedTalkgroup(t, 262, "TG262")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		gw, err := testutils.NewSessionPeer(100001, testPassword, "StartRef=4013;RelinkTime=15;TS1_1=262;TS2_1=10;")
		require.NoError(t, err)
		require.NoError(t, gw.Connect(second.MMDVMAddr))
		defer gw.Close()
		require.NoError(t, gw.WaitReady(handshakeWait))
		waitStatics(t, stack, 100001, []uint{262}, []uint{10})

		rpt := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, rpt.Connect(stack.MMDVMAddr))
		defer rpt.Close()
		require.NoError(t, rpt.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		sendGroupCall(t, rpt.SendDMRD, 10, 9201, true)
		pkts := gw.Drain(drainWait)
		require.NotEmpty(t, pkts, "the peer on the second replica should hear its TS2 talkgroup")
		for _, p := range pkts {
			assert.Equal(t, uint(10), p.Dst)
			assert.True(t, p.Slot)
		}

		require.NoError(t, second.MMDVMServer.Stop(servers.WithGracefulHandoff(context.Background(), true)))
		require.NoError(t, gw.Retarget(stack.MMDVMAddr))
		time.Sleep(settleDuration)
		require.Equal(t, dmrdsession.StateRunning, gw.State())
		assert.Zero(t, gw.Downs(), "the first replica should resume the session")

		time.Sleep(callHoldTime)
		sendGroupCall(t, rpt.SendDMRD, 262, 9203, false)
		pkts = gw.Drain(drainWait)
		require.NotEmpty(t, pkts, "the peer should hear its TS1 talkgroup after it moved to the first replica")
		for _, p := range pkts {
			assert.Equal(t, uint(262), p.Dst)
			assert.False(t, p.Slot)
		}
	})
}

func dynamicIDs(t *testing.T, stack *testutils.IntegrationStack, id uint) (ts1, ts2 uint) {
	t.Helper()
	rpt, err := models.FindRepeaterByID(stack.DB, id)
	require.NoError(t, err)
	if rpt.TS1DynamicTalkgroupID != nil {
		ts1 = *rpt.TS1DynamicTalkgroupID
	}
	if rpt.TS2DynamicTalkgroupID != nil {
		ts2 = *rpt.TS2DynamicTalkgroupID
	}
	return ts1, ts2
}

func waitDynamic(t *testing.T, stack *testutils.IntegrationStack, id, ts1, ts2 uint) {
	t.Helper()
	require.Eventually(t, func() bool {
		got1, got2 := dynamicIDs(t, stack, id)
		return got1 == ts1 && got2 == ts2
	}, handshakeWait, 20*time.Millisecond, "dynamic talkgroups of %d should become TS1 %d TS2 %d", id, ts1, ts2)
}

// TestRPTODialLinksDynamicTalkgroup sends DIAL in the options. The
// talkgroup must be linked as the dynamic talkgroup of TS2 and routed, a
// keyup on another talkgroup must relink the slot as usual, and DIAL=0
// must unlink it.
func TestRPTODialLinksDynamicTalkgroup(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 10, "TG10")
		stack.SeedTalkgroup(t, 11, "TG11")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))

		require.NoError(t, c2.SendOptions("DIAL=10;"))
		waitDynamic(t, stack, 100002, 0, 10)
		time.Sleep(settleDuration)

		sendGroupCall(t, c1.SendDMRD, 10, 9301, true)
		pkts := c2.Drain(drainWait)
		require.NotEmpty(t, pkts, "the repeater should hear the talkgroup that DIAL linked")
		for _, p := range pkts {
			assert.Equal(t, uint(10), p.Dst)
			assert.True(t, p.Slot)
		}

		time.Sleep(callHoldTime)
		sendGroupCall(t, c2.SendDMRD, 11, 9302, true)
		waitDynamic(t, stack, 100002, 0, 11)
		time.Sleep(settleDuration)

		require.NoError(t, c2.SendOptions("DIAL=0;"))
		waitDynamic(t, stack, 100002, 0, 0)
	})
}
