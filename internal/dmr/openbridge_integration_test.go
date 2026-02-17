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
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"slices"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const obPassword = "obTestPass123"

// TestOpenBridgeInboundPeerToLocalRepeater verifies that an OpenBridge peer
// sending a group call packet is delivered to an MMDVM repeater subscribed
// to the same talkgroup.
func TestOpenBridgeInboundPeerToLocalRepeater(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		// Peer with ingress enabled, ingress rule covers TG1
		stack.SeedPeer(t, 500001, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, 500001, true, 1, 1) // ingress rule for TG1
		stack.StartServers(t)

		// Connect MMDVM client
		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		// Connect OpenBridge client
		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))

		// Send group voice from the OB peer
		err := obc.SendGroupVoice(1000001, 1, 7001)
		require.NoError(t, err)

		// MMDVM client should receive the packet
		pkts := mc.Drain(drainWait)
		assert.NotEmpty(t, pkts, "MMDVM client should receive packets from OpenBridge peer")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst)
			assert.True(t, p.GroupCall)
		}
	})
}

// TestOpenBridgeOutboundLocalRepeaterToPeer verifies that an MMDVM repeater
// sending a group call delivers the packet to an OpenBridge peer with a
// matching egress rule, including a valid HMAC.
func TestOpenBridgeOutboundLocalRepeaterToPeer(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.StartServers(t)

		// Connect OpenBridge client first so we know its local address
		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))
		localAddr := obc.LocalAddr()
		require.NotNil(t, localAddr)

		// Seed peer with the client's actual local address for outbound delivery
		stack.SeedPeer(t, 500001, 1000001, obPassword, localAddr.IP.String(), localAddr.Port, false, true)
		stack.SeedPeerRule(t, 500001, false, 1000001, 1000001) // egress rule matching src ID

		// Connect MMDVM client
		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		// MMDVM client sends group voice to TG1
		voicePkt := makeGroupVoicePacket(1000001, 1, 8001, false)
		require.NoError(t, mc.SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makeGroupVoiceTermPacket(1000001, 1, 8001, false)
		require.NoError(t, mc.SendDMRD(termPkt))

		// OpenBridge client should receive the packet with valid HMAC
		pkts := obc.Drain(drainWait)
		assert.NotEmpty(t, pkts, "OpenBridge peer should receive outbound packets")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst)
			assert.True(t, p.GroupCall)
			assert.False(t, p.Slot, "OpenBridge is always TS1")
		}
	})
}

// TestOpenBridgePeerToPeerForwarding verifies that a packet from one OpenBridge
// peer is forwarded to another OpenBridge peer with a matching egress rule.
func TestOpenBridgePeerToPeerForwarding(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.StartServers(t)

		// Connect OB client B first to get its local address
		obcB := testutils.NewOpenBridgeClient(500002, obPassword)
		defer obcB.Close()
		require.NoError(t, obcB.Connect(stack.OpenBridgeAddr))
		localAddrB := obcB.LocalAddr()
		require.NotNil(t, localAddrB)

		// Peer A: ingress enabled, ingress rule covers TG1
		stack.SeedPeer(t, 500001, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, 500001, true, 1, 1) // ingress for TG1

		// Peer B: egress enabled, egress rule covers the source ID
		stack.SeedPeer(t, 500002, 1000001, obPassword, localAddrB.IP.String(), localAddrB.Port, false, true)
		stack.SeedPeerRule(t, 500002, false, 1000001, 1000001) // egress for src

		// Connect OB client A
		obcA := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obcA.Close()
		require.NoError(t, obcA.Connect(stack.OpenBridgeAddr))
		time.Sleep(settleDuration)

		// OB client A sends a group call
		err := obcA.SendGroupVoice(1000001, 1, 9001)
		require.NoError(t, err)

		// OB client B should receive the forwarded packet
		pktsB := obcB.Drain(drainWait)
		assert.NotEmpty(t, pktsB, "Peer B should receive forwarded packets from Peer A")
	})
}

// TestOpenBridgeIngressRuleRejection verifies that an OpenBridge peer's packet
// is not delivered to local repeaters if the TG is not in its ingress rules.
func TestOpenBridgeIngressRuleRejection(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedTalkgroup(t, 2, "TG2")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		// Peer with ingress enabled but rule only covers TG2 (not TG1)
		stack.SeedPeer(t, 500001, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, 500001, true, 2, 2) // ingress rule for TG2 only
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))

		// Send to TG1 — should be rejected by ingress rules
		err := obc.SendGroupVoice(1000001, 1, 6001)
		require.NoError(t, err)

		pkts := mc.Drain(drainWait)
		assert.Empty(t, pkts, "MMDVM should NOT receive packets that fail ingress rules")
	})
}

// TestOpenBridgeEgressRuleRejection verifies that an MMDVM packet is not
// delivered to an OpenBridge peer when the egress rule doesn't match.
func TestOpenBridgeEgressRuleRejection(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.StartServers(t)

		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))
		localAddr := obc.LocalAddr()
		require.NotNil(t, localAddr)

		// Peer with egress enabled but rule covers a different source range
		stack.SeedPeer(t, 500001, 1000001, obPassword, localAddr.IP.String(), localAddr.Port, false, true)
		stack.SeedPeerRule(t, 500001, false, 9999999, 9999999) // egress rule that won't match

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		voicePkt := makeGroupVoicePacket(1000001, 1, 5001, false)
		require.NoError(t, mc.SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makeGroupVoiceTermPacket(1000001, 1, 5001, false)
		require.NoError(t, mc.SendDMRD(termPkt))

		pkts := obc.Drain(drainWait)
		assert.Empty(t, pkts, "OpenBridge peer should NOT receive packets that fail egress rules")
	})
}

// TestOpenBridgeInvalidHMACRejection verifies that packets with invalid HMAC
// are dropped and not routed.
func TestOpenBridgeInvalidHMACRejection(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.SeedPeer(t, 500001, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, 500001, true, 1, 1)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		// Connect with the WRONG password — HMAC will be invalid
		obcBad := testutils.NewOpenBridgeClient(500001, "wrongPassword")
		defer obcBad.Close()
		require.NoError(t, obcBad.Connect(stack.OpenBridgeAddr))

		err := obcBad.SendGroupVoice(1000001, 1, 4001)
		require.NoError(t, err)

		pkts := mc.Drain(drainWait)
		assert.Empty(t, pkts, "MMDVM should NOT receive packets with invalid HMAC")
	})
}

// TestOpenBridgeUnknownPeerRejection verifies that packets from an unregistered
// peer ID are dropped.
func TestOpenBridgeUnknownPeerRejection(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		// Do NOT seed a peer — the peer ID will be unknown
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		obc := testutils.NewOpenBridgeClient(500099, "anyPassword")
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))

		err := obc.SendGroupVoice(1000001, 1, 3001)
		require.NoError(t, err)

		pkts := mc.Drain(drainWait)
		assert.Empty(t, pkts, "MMDVM should NOT receive packets from unknown peer")
	})
}

// TestOpenBridgeTS2Drop verifies that TS2 packets from an OpenBridge peer
// are dropped (OpenBridge is TS1-only).
func TestOpenBridgeTS2Drop(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.SeedPeer(t, 500001, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, 500001, true, 1, 1)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))

		// Send a TS2 packet manually
		ts2Pkt := models.Packet{
			Signature:   string(dmrconst.CommandDMRD),
			Seq:         1,
			Src:         1000001,
			Dst:         1,
			Repeater:    500001,
			GroupCall:   true,
			Slot:        true, // TS2 — should be dropped by OpenBridge
			FrameType:   dmrconst.FrameVoice,
			DTypeOrVSeq: dmrconst.VoiceA,
			StreamID:    2001,
			BER:         -1,
			RSSI:        -1,
		}
		// Build the packet manually with HMAC
		encoded := ts2Pkt.Encode()[:dmrconst.MMDVMPacketLength]
		h := hmac.New(sha1.New, []byte(obPassword))
		_, _ = h.Write(encoded)
		outbound := slices.Concat(encoded, h.Sum(nil))
		require.NoError(t, obc.SendRawBytes(outbound))

		pkts := mc.Drain(drainWait)
		assert.Empty(t, pkts, "MMDVM should NOT receive TS2 packets from OpenBridge peer")
	})
}

// TestOpenBridgeCrossProtocolIPSCToOpenBridge verifies that an IPSC client
// sending a group call is delivered to an OpenBridge peer and vice versa.
func TestOpenBridgeCrossProtocolIPSCToOpenBridge(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		// IPSC repeater subscribed to TG1
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.AssignTS1StaticTG(t, 200001, 1)
		stack.StartServers(t)

		// Connect OB client first to get its local address
		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))
		localAddr := obc.LocalAddr()
		require.NotNil(t, localAddr)

		// OB peer with both ingress (TG1) and egress (src range)
		stack.SeedPeer(t, 500001, 1000001, obPassword, localAddr.IP.String(), localAddr.Port, true, true)
		stack.SeedPeerRule(t, 500001, true, 1, 1)              // ingress TG1
		stack.SeedPeerRule(t, 500001, false, 1000001, 1000001) // egress src

		// Connect IPSC client
		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		defer ic.Close()
		require.NoError(t, ic.Connect(stack.IPSCAddr))
		require.NoError(t, ic.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		// IPSC → OpenBridge: IPSC sends group voice, OB peer should receive
		err := ic.SendGroupVoice(1000001, 1, false, 11001)
		require.NoError(t, err)
		obPkts := obc.Drain(drainWait)
		assert.NotEmpty(t, obPkts, "OpenBridge peer should receive packets from IPSC client")

		// OpenBridge → IPSC: OB peer sends group voice, IPSC client should receive
		err = obc.SendGroupVoice(1000001, 1, 12001)
		require.NoError(t, err)
		ipscPkts := ic.Drain(drainWait)
		assert.NotEmpty(t, ipscPkts, "IPSC client should receive packets from OpenBridge peer")
	})
}
