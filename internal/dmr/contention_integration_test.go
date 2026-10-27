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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoTalkers logs in the hotspots 100001 and 100002 of two users and the
// listener 100003, which all carry TG 3100 on TS1, and has both talkers
// key up TG 3100 at once, their bursts interleaved, the first talker
// 30 ms ahead. It returns the stream
// IDs that the listener got, with the count of bursts of each, and the
// clients of the talkers and the listener.
func twoTalkers(t *testing.T, stack *testutils.IntegrationStack) (map[uint]int, []*testutils.MMDVMClient) {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedUser(t, 1000002, "N1CALL")
	stack.SeedTalkgroup(t, 3100, "TG")
	for i, id := range []uint{100001, 100002, 100003} {
		owner := uint(1000001)
		if i == 1 {
			owner = 1000002
		}
		stack.SeedMMDVMRepeater(t, id, owner, testPassword)
		stack.AssignTS1StaticTG(t, id, 3100)
	}
	stack.StartServers(t)
	clients := make([]*testutils.MMDVMClient, 0, 3)
	for _, id := range []uint32{100001, 100002, 100003} {
		c := testutils.NewMMDVMClient(id, "", testPassword)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		t.Cleanup(c.Close)
		require.NoError(t, c.WaitReady(handshakeWait))
		clients = append(clients, c)
	}
	time.Sleep(settleDuration)

	burst := func(src, stream uint, seq int) models.Packet {
		p := makeGroupVoicePacket(src, 3100, stream, false)
		p.Seq = uint(seq)
		p.DTypeOrVSeq = uint(seq % 6)
		if seq%6 == 0 {
			p.FrameType = dmrconst.FrameVoiceSync
		}
		return p
	}
	const bursts = 6
	for i := range bursts {
		require.NoError(t, clients[0].SendDMRD(burst(1000001, 1001, i)))
		time.Sleep(30 * time.Millisecond)
		require.NoError(t, clients[1].SendDMRD(burst(1000002, 1002, i)))
		time.Sleep(30 * time.Millisecond)
	}
	a := makeGroupVoiceTermPacket(1000001, 3100, 1001, false)
	a.Seq = bursts
	b := makeGroupVoiceTermPacket(1000002, 3100, 1002, false)
	b.Seq = bursts
	require.NoError(t, clients[0].SendDMRD(a))
	require.NoError(t, clients[1].SendDMRD(b))

	got := map[uint]int{}
	for _, p := range clients[2].Drain(drainWait) {
		if p.Dst == 3100 {
			got[p.StreamID]++
		}
	}
	return got, clients
}

// TestTwoTalkersOnOneTalkgroupMix documents what DMRHub does by default
// when two radios key up the same talkgroup at once: it sends the bursts
// of both streams to the repeaters that carry the talkgroup, interleaved.
func TestTwoTalkersOnOneTalkgroupMix(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{})
		got, _ := twoTalkers(t, stack)
		assert.Equal(t, 7, got[1001], "the listener gets every burst of the first talker")
		assert.Equal(t, 7, got[1002], "the listener gets every burst of the second talker too")
	})
}

// TestTwoTalkersFirstHoldsTheTimeslot has the first talker hold the
// timeslot of each listener until its call ends. The second call does not
// reach the listener, and a call after the first one ends does.
func TestTwoTalkersFirstHoldsTheTimeslot(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{Contention: config.ContentionFirst})
		got, clients := twoTalkers(t, stack)
		assert.Equal(t, 7, got[1001], "the listener gets every burst of the first talker")
		assert.Zero(t, got[1002], "the listener gets nothing of the second talker while the first one talks")

		require.NoError(t, clients[1].SendDMRD(makeGroupVoicePacket(1000002, 3100, 1003, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, clients[1].SendDMRD(makeGroupVoiceTermPacket(1000002, 3100, 1003, false)))
		later := 0
		for _, p := range clients[2].Drain(drainWait) {
			if p.StreamID == 1003 {
				later++
			}
		}
		assert.Equal(t, 2, later, "a call after the first one ends reaches the listener")
	})
}
