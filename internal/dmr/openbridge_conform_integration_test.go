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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	conformUser     = 3000001
	conformRepeater = 300001
	conformPeer     = 3000002
)

type conformStack struct {
	mc   *testutils.MMDVMClient
	peer *testutils.OpenBridgeClient
}

// startConformStack links a peer, enhanced or not, whose ingress rule
// takes TG 1 and the repeater conformRepeater, to a hub with that repeater
// on TG 1.
func startConformStack(t *testing.T, stack *testutils.IntegrationStack, enhanced bool) conformStack {
	t.Helper()
	stack.SeedUser(t, conformUser, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedTalkgroup(t, 2, "TG2")
	stack.SeedMMDVMRepeater(t, conformRepeater, conformUser, testPassword)
	stack.AssignTS1StaticTG(t, conformRepeater, 1)
	stack.AssignTS1StaticTG(t, conformRepeater, 2)
	stack.StartServers(t)

	peer := testutils.NewOpenBridgeClient(conformPeer, obPassword)
	t.Cleanup(peer.Close)
	require.NoError(t, peer.Connect(stack.OpenBridgeAddr))
	addr := peer.LocalAddr()
	stack.SeedPeer(t, conformPeer, conformUser, obPassword, addr.IP.String(), addr.Port, true, true)
	require.NoError(t, stack.DB.Model(&models.Peer{}).Where("id = ?", conformPeer).Update("enhanced", enhanced).Error)
	stack.SeedPeerRule(t, conformPeer, true, 1, 1)
	stack.SeedPeerRule(t, conformPeer, true, conformRepeater, conformRepeater)
	stack.SeedPeerRule(t, conformPeer, false, 1, dmrconst.MaxDMRAddress)

	mc := testutils.NewMMDVMClient(conformRepeater, "", testPassword)
	t.Cleanup(mc.Close)
	require.NoError(t, mc.Connect(stack.MMDVMAddr))
	require.NoError(t, mc.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	return conformStack{mc: mc, peer: peer}
}

func quenches(controls []dmrd.BridgeControl, stream uint32) int {
	n := 0
	for _, c := range controls {
		if c.Command == dmrd.CommandBCSQ && c.Stream == stream {
			n++
		}
	}
	return n
}

// TestOpenBridgeDropsStaleEnhancedPackets sends a DMRE packet whose send
// time is 6 s old, as FreeDMR drops one over 5 s old (hblink.py "more than
// 5s old"). The hub must drop it and quench the stream, and still take a
// fresh one.
func TestOpenBridgeDropsStaleEnhancedPackets(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startConformStack(t, stack, true)
		key := dmrdauth.NewBridgeKey([]byte(obPassword))
		send := func(stream uint, age time.Duration) {
			p := makeGroupVoicePacket(conformUser, 1, stream, false)
			p.Repeater = conformPeer
			b := dmrd.Bridge{Version: 5, Data: p.Data(), Time: uint64(time.Now().Add(-age).UnixNano()), SourceServer: conformPeer, Hops: 1} //nolint:gosec
			b.Data.Size = dmrconst.MMDVMMaxPacketLength
			out, err := b.AppendBinary(nil)
			require.NoError(t, err)
			require.NoError(t, key.SignInPlace(out))
			require.NoError(t, s.peer.SendRawBytes(out))
		}

		send(9601, 6*time.Second)
		assert.Empty(t, s.mc.Drain(drainWait), "a stale DMRE packet reached the repeater")
		assert.Positive(t, quenches(s.peer.DrainControls(300*time.Millisecond), 9601), "the stale stream was not quenched")

		send(9602, 0)
		assert.Len(t, s.mc.Drain(drainWait), 1, "a fresh DMRE packet must reach the repeater")
	})
}

// TestOpenBridgeTakesAPrivateCallOnTimeslot2 sends a private call on TS2,
// as HBlink3 does to a peer with BOTH_SLOTS (bridge.py unit_received). The
// hub must take it, as hblink.py does, and still drop a group call on TS2.
func TestOpenBridgeTakesAPrivateCallOnTimeslot2(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startConformStack(t, stack, false)

		group := makeGroupVoicePacket(conformUser, 1, 9611, true)
		group.Repeater = conformPeer
		require.NoError(t, s.peer.SendRawPacket(group))
		assert.Empty(t, s.mc.Drain(drainWait), "a group call on TS2 reached the repeater")

		private := makePrivateVoicePacket(conformRepeater, 9612)
		private.Src = conformUser
		private.Slot = true
		private.Repeater = conformPeer
		require.NoError(t, s.peer.SendRawPacket(private))
		got := s.mc.Drain(drainWait)
		if assert.Len(t, got, 1, "a private call on TS2 must reach the repeater") {
			assert.Equal(t, uint(conformRepeater), got[0].Dst)
			assert.False(t, got[0].GroupCall)
		}
	})
}

// TestOpenBridgeQuenchesAStreamItsRulesRefuse sends an enhanced peer's
// call to a talkgroup that its ingress rules do not take. FreeDMR answers
// a call that its ACLs refuse with a BCSQ, so the peer stops sending it;
// the hub must too.
func TestOpenBridgeQuenchesAStreamItsRulesRefuse(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startConformStack(t, stack, true)
		p := makeGroupVoicePacket(conformUser, 2, 9621, false)
		p.Repeater = conformPeer
		require.NoError(t, s.peer.SendEnhancedPacket(p, 5))
		assert.Empty(t, s.mc.Drain(drainWait), "a call the rules refuse reached the repeater")
		assert.Positive(t, quenches(s.peer.DrainControls(300*time.Millisecond), 9621), "the refused stream was not quenched")
	})
}

// TestOpenBridgeSendsItsVersion links an enhanced peer. FreeDMR sends a
// BCVE of its version at the start of the link and every minute
// (hblink.py send_bcve), so a peer that runs an older version learns the
// newest that it may send; the hub must send its own.
func TestOpenBridgeSendsItsVersion(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startConformStack(t, stack, true)
		var versions []uint8
		for _, c := range s.peer.DrainControls(3 * time.Second) {
			if c.Command == dmrd.CommandBCVE {
				versions = append(versions, c.Version)
			}
		}
		assert.Contains(t, versions, uint8(5), "the hub sent no BCVE of version 5")
	})
}

// TestOpenBridgeMovesALinkUpToThePeersVersion links a peer as version 1,
// which then sends a BCVE of version 5, as FreeDMR does to a link that
// sends it DMRD while its PROTO_VER is 5 (hblink.py "Version 1 protocol
// prohibited"), and drops every DMRD until the link moves up. The hub must
// move the link up as FreeDMR does on a BCVE: send DMRE of version 5 and
// BCKA keepalives from then on.
func TestOpenBridgeMovesALinkUpToThePeersVersion(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startConformStack(t, stack, false)
		send := func(stream uint) []testutils.ReceivedBridge {
			require.NoError(t, s.mc.SendDMRD(makeGroupVoicePacket(conformUser, 1, stream, false)))
			return s.peer.DrainBridges(drainWait)
		}
		got := send(9631)
		require.Len(t, got, 1)
		require.Equal(t, uint8(1), got[0].Version, "the link starts at version 1")

		require.NoError(t, s.peer.SendControl(dmrd.BridgeControl{Command: dmrd.CommandBCVE, Version: 5}))
		time.Sleep(200 * time.Millisecond)
		got = send(9632)
		if assert.Len(t, got, 1) {
			assert.Equal(t, uint8(5), got[0].Version, "the hub did not move the link up to version 5")
		}
		keepalives := 0
		for _, c := range s.peer.DrainControls(2 * time.Second) {
			if c.Command == dmrd.CommandBCKA {
				keepalives++
			}
		}
		assert.Positive(t, keepalives, "the hub sends no keepalives on the link that moved up")
	})
}

// moveUp sends the hub a BCVE of version 5 from the peer of s, and checks
// that the next call goes to the peer as DMRE of version 5.
func (s conformStack) moveUp(t *testing.T, stream uint) {
	t.Helper()
	require.NoError(t, s.peer.SendControl(dmrd.BridgeControl{Command: dmrd.CommandBCVE, Version: 5}))
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, []uint8{5}, s.versionsOf(t, stream), "the hub moved the link up to version 5")
}

// versionsOf sends a call from the repeater of s and returns the version
// of each packet of it that reaches the peer.
func (s conformStack) versionsOf(t *testing.T, stream uint) []uint8 {
	t.Helper()
	require.NoError(t, s.mc.SendDMRD(makeGroupVoicePacket(conformUser, 1, stream, false)))
	got := dataOf(s.peer.DrainBridges(drainWait), uint32(stream)) //nolint:gosec
	out := make([]uint8, 0, len(got))
	for _, b := range got {
		out = append(out, b.Version)
	}
	return out
}

// TestOpenBridgeForgetsTheVersionOfAChangedPeer moves a version 1 link up
// with a BCVE, then changes the peer, as an admin does when they edit it or
// delete it and add it again. The hub must send version 1 again until the
// peer names a newer one.
func TestOpenBridgeForgetsTheVersionOfAChangedPeer(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startConformStack(t, stack, false)

		s.moveUp(t, 9641)
		require.NoError(t, stack.DB.Model(&models.Peer{}).Where("id = ?", conformPeer).Update("enhanced", false).Error)
		stack.Hub.PeerChanged(conformPeer)
		time.Sleep(200 * time.Millisecond)
		assert.Equal(t, []uint8{1}, s.versionsOf(t, 9642), "an edit of the peer keeps the version that the peer named before it")

		s.moveUp(t, 9643)
		require.NoError(t, models.DeletePeer(stack.DB, conformPeer))
		stack.Hub.PeerChanged(conformPeer)
		addr := s.peer.LocalAddr()
		stack.SeedPeer(t, conformPeer, conformUser, obPassword, addr.IP.String(), addr.Port, true, true)
		stack.SeedPeerRule(t, conformPeer, false, 1, dmrconst.MaxDMRAddress)
		stack.Hub.PeerChanged(conformPeer)
		time.Sleep(200 * time.Millisecond)
		assert.Equal(t, []uint8{1}, s.versionsOf(t, 9644), "a peer added again keeps the version of the deleted one")
	})
}

// TestOpenBridgeReplicaThatStartsLaterKnowsTheLinkVersion moves a version 1
// link up with a BCVE, then starts a second replica and stops the first, as
// a rolling restart does. The second replica must send DMRE of version 5:
// FreeDMR drops the DMRD of a link that it moved up.
func TestOpenBridgeReplicaThatStartsLaterKnowsTheLinkVersion(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startConformStack(t, stack, false)
		s.moveUp(t, 9651)

		const lateRepeater = conformRepeater + 1
		stack.SeedMMDVMRepeater(t, lateRepeater, conformUser, testPassword)
		second := stack.SpawnSecondReplica(t)
		mc := testutils.NewMMDVMClient(lateRepeater, "", testPassword)
		t.Cleanup(mc.Close)
		require.NoError(t, mc.Connect(second.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		require.NoError(t, stack.OpenBridgeServer.Stop(t.Context()))

		var got []testutils.ReceivedBridge
		stream := uint32(9652)
		for start := time.Now(); len(got) == 0 && time.Since(start) < 3*openbridge.EgressLeaseTTL; stream++ {
			require.NoError(t, mc.SendDMRD(makeGroupVoicePacket(conformUser, 1, uint(stream), false)))
			got = dataOf(s.peer.DrainBridges(100*time.Millisecond), stream)
		}
		if assert.NotEmpty(t, got, "the second replica sends to the peer") {
			assert.Equal(t, uint8(5), got[0].Version, "the second replica sends the version that the peer named")
		}
	})
}
