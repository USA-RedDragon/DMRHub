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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type egressRuleStack struct {
	mc   *testutils.MMDVMClient
	peer *testutils.OpenBridgeClient
}

func startEgressRuleStack(t *testing.T, stack *testutils.IntegrationStack, seedRules func(peerID uint)) egressRuleStack {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedUser(t, 1000002, "N0CALM")
	stack.SeedTalkgroup(t, 91, "TG91")
	stack.SeedTalkgroup(t, 3100, "TG3100")
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	stack.StartServers(t)

	peer := testutils.NewOpenBridgeClient(500001, obPassword)
	t.Cleanup(peer.Close)
	require.NoError(t, peer.Connect(stack.OpenBridgeAddr))
	addr := peer.LocalAddr()
	stack.SeedPeer(t, 500001, 1000001, obPassword, addr.IP.String(), addr.Port, false, true)
	seedRules(500001)

	mc := testutils.NewMMDVMClient(100001, "", testPassword)
	t.Cleanup(mc.Close)
	require.NoError(t, mc.Connect(stack.MMDVMAddr))
	require.NoError(t, mc.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	return egressRuleStack{mc: mc, peer: peer}
}

func (s egressRuleStack) groupCallReaches(t *testing.T, src, tg, stream uint) bool {
	t.Helper()
	require.NoError(t, s.mc.SendDMRD(makeGroupVoicePacket(src, tg, stream, false)))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, s.mc.SendDMRD(makeGroupVoiceTermPacket(src, tg, stream, false)))
	got := s.peer.Drain(drainWait)
	for _, p := range got {
		assert.Equal(t, tg, p.Dst)
		assert.True(t, p.GroupCall)
	}
	return len(got) > 0
}

func (s egressRuleStack) privateCallReaches(t *testing.T, dst, stream uint) bool {
	t.Helper()
	require.NoError(t, s.mc.SendDMRD(makeBurst(1000001, dst, stream, false, dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader), 0)))
	got := s.peer.Drain(drainWait)
	for _, p := range got {
		assert.Equal(t, dst, p.Dst)
		assert.False(t, p.GroupCall)
	}
	return len(got) > 0
}

// TestOpenBridgeEgressTalkgroupRule gives a peer one transmit rule for TG
// 3100. The peer must get the calls to TG 3100 from any radio and no other
// call, private calls included.
func TestOpenBridgeEgressTalkgroupRule(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startEgressRuleStack(t, stack, func(peerID uint) {
			stack.SeedPeerRuleMatch(t, peerID, false, models.PeerRuleMatchDestination, 3100, 3100)
		})
		assert.True(t, s.groupCallReaches(t, 1000001, 3100, 9601), "TG 3100 must reach the peer")
		assert.False(t, s.groupCallReaches(t, 1000001, 91, 9602), "TG 91 must not reach the peer")
		assert.True(t, s.groupCallReaches(t, 1000002, 3100, 9603), "TG 3100 from another radio must reach the peer")
		assert.False(t, s.privateCallReaches(t, 3999999, 9604), "a private call must not reach the peer")
	})
}

// TestOpenBridgeEgressSourceAndTalkgroupRules gives a peer transmit rules
// of both kinds. A call must pass both: a talkgroup or private target in a
// destination rule, and a caller in a source rule.
func TestOpenBridgeEgressSourceAndTalkgroupRules(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startEgressRuleStack(t, stack, func(peerID uint) {
			stack.SeedPeerRuleMatch(t, peerID, false, models.PeerRuleMatchDestination, 3100, 3100)
			stack.SeedPeerRuleMatch(t, peerID, false, models.PeerRuleMatchDestination, 3000000, 3999999)
			stack.SeedPeerRuleMatch(t, peerID, false, models.PeerRuleMatchSource, 1000001, 1000001)
		})
		assert.True(t, s.groupCallReaches(t, 1000001, 3100, 9701), "TG 3100 from 1000001 must reach the peer")
		assert.False(t, s.groupCallReaches(t, 1000001, 91, 9702), "TG 91 must not reach the peer")
		assert.False(t, s.groupCallReaches(t, 1000002, 3100, 9703), "a call from 1000002 must not reach the peer")
		assert.True(t, s.privateCallReaches(t, 3999999, 9704), "a private call to 3999999 must reach the peer")
		assert.False(t, s.privateCallReaches(t, 4000001, 9705), "a private call to 4000001 must not reach the peer")
	})
}

// TestOpenBridgeEgressSourceRule keeps the transmit rules made before rules
// had a match: they check the caller, whatever the talkgroup.
func TestOpenBridgeEgressSourceRule(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startEgressRuleStack(t, stack, func(peerID uint) {
			stack.SeedPeerRule(t, peerID, false, 1000001, 1000001)
		})
		assert.True(t, s.groupCallReaches(t, 1000001, 91, 9801), "TG 91 from 1000001 must reach the peer")
		assert.True(t, s.groupCallReaches(t, 1000001, 3100, 9802), "TG 3100 from 1000001 must reach the peer")
		assert.False(t, s.groupCallReaches(t, 1000002, 3100, 9803), "a call from 1000002 must not reach the peer")
	})
}

// TestOpenBridgeEgressPrivateCalls gives a peer a transmit rule for TG 3100
// and turns its private call switch on and off. On, the peer gets private
// calls to any radio; off, it gets none. Group calls follow the rule.
func TestOpenBridgeEgressPrivateCalls(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startEgressRuleStack(t, stack, func(peerID uint) {
			stack.SeedPeerRuleMatch(t, peerID, false, models.PeerRuleMatchDestination, 3100, 3100)
			stack.SetPeerPrivateCalls(t, peerID, false, true)
		})
		assert.True(t, s.privateCallReaches(t, 3999999, 9901), "with the switch on, a private call must reach the peer")
		assert.False(t, s.groupCallReaches(t, 1000001, 91, 9902), "with the switch on, TG 91 must not reach the peer")
		assert.True(t, s.groupCallReaches(t, 1000001, 3100, 9903), "TG 3100 must reach the peer")

		stack.SetPeerPrivateCalls(t, 500001, false, false)
		time.Sleep(settleDuration)
		assert.False(t, s.privateCallReaches(t, 3999998, 9904), "with the switch off, a private call must not reach the peer")
	})
}

// TestOpenBridgeIngressPrivateCalls gives a peer a receive rule for TG 3100
// and sends a private call to the local repeater, with the peer's private
// call switch on and then off.
func TestOpenBridgeIngressPrivateCalls(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 3100, "TG3100")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedPeer(t, 500001, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRuleMatch(t, 500001, true, models.PeerRuleMatchDestination, 3100, 3100)
		stack.SetPeerPrivateCalls(t, 500001, true, false)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))
		time.Sleep(settleDuration)

		send := func(stream uint) []models.Packet {
			p := makePrivateVoicePacket(100001, stream)
			p.Src = 3999999
			p.Repeater = 500001
			require.NoError(t, obc.SendRawPacket(p))
			return mc.Drain(drainWait)
		}
		got := send(9911)
		if assert.Len(t, got, 1, "with the switch on, the private call must reach the repeater") {
			assert.Equal(t, uint(100001), got[0].Dst)
			assert.False(t, got[0].GroupCall)
		}

		stack.SetPeerPrivateCalls(t, 500001, false, false)
		time.Sleep(settleDuration)
		assert.Empty(t, send(9912), "with the switch off, the private call must not reach the repeater")
	})
}
