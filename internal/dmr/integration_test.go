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

// Package dmr_test contains end-to-end routing integration tests that exercise
// the full packet path: real UDP sockets → MMDVM/IPSC protocol servers → Hub
// routing → pubsub fan-out → UDP delivery to destination clients.
//
// These tests verify that packets are delivered ONLY to the correct recipients
// and NOT to unrelated repeaters.
package dmr_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPassword   = "s3cret"
	ipscAuthKey    = "0000000000000000000000000000000000003039" // hex auth key for IPSC peers
	handshakeWait  = 5 * time.Second
	drainWait      = 2 * time.Second
	settleDuration = 500 * time.Millisecond
)

// allBackends returns the list of backends to test against.
func allBackends() []testutils.Backend {
	return []testutils.Backend{
		testutils.SQLiteMemoryBackend(),
		testutils.PostgresRedisBackend(),
	}
}

// forAllBackends runs f as a subtest against every configured backend.
func forAllBackends(t *testing.T, f func(t *testing.T, stack *testutils.IntegrationStack)) {
	t.Helper()
	for _, be := range allBackends() {
		be := be
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			stack := testutils.SetupIntegrationStack(t, be)
			f(t, stack)
		})
	}
}

// makeGroupVoicePacket builds a minimal MMDVM-style group voice packet
// suitable for routing through the hub.
func makeGroupVoicePacket(src, dst, streamID uint, slot bool) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         1,
		Src:         src,
		Dst:         dst,
		GroupCall:   true,
		Slot:        slot,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA,
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
}

// makeGroupVoiceTermPacket builds a group voice terminator packet.
func makeGroupVoiceTermPacket(src, dst, streamID uint, slot bool) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         2,
		Src:         src,
		Dst:         dst,
		GroupCall:   true,
		Slot:        slot,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceTerm),
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
}

// makePrivateVoicePacket builds a private voice packet.
func makePrivateVoicePacket(dst, streamID uint) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         1,
		Src:         1000001,
		Dst:         dst,
		GroupCall:   false,
		Slot:        false,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA,
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
}

// makePrivateVoiceTermPacket builds a private voice terminator packet.
func makePrivateVoiceTermPacket(dst, streamID uint) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         2,
		Src:         1000001,
		Dst:         dst,
		GroupCall:   false,
		Slot:        false,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceTerm),
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
}

// TestGroupCallMMDVMToMMDVM verifies that a group voice call from one MMDVM
// repeater is delivered to another MMDVM repeater subscribed to the same
// talkgroup, and NOT delivered to an MMDVM repeater on a different talkgroup.
func TestGroupCallMMDVMToMMDVM(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		// Seed: user, two talkgroups, three repeaters
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedTalkgroup(t, 2, "TG2")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100003, 1000001, testPassword)

		// R1 and R2 get TG1 on TS1; R3 gets only TG2 on TS2.
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.AssignTS2StaticTG(t, 100003, 2)

		stack.StartServers(t)

		// Connect three MMDVM clients
		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		c3 := testutils.NewMMDVMClient(100003, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		require.NoError(t, c3.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		defer c3.Close()

		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))
		require.NoError(t, c3.WaitReady(handshakeWait))

		// Allow subscriptions to settle
		time.Sleep(settleDuration)

		// C1 sends a group voice call to TG1 on TS1
		voicePkt := makeGroupVoicePacket(1000001, 1, 42, false)
		require.NoError(t, c1.SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makeGroupVoiceTermPacket(1000001, 1, 42, false)
		require.NoError(t, c1.SendDMRD(termPkt))

		// C2 should receive the packet (subscribed to TG1 on TS1)
		pkts2 := c2.Drain(drainWait)
		assert.NotEmpty(t, pkts2, "C2 should receive packets on TG1")
		for _, p := range pkts2 {
			assert.Equal(t, uint(1), p.Dst, "packet destination should be TG1")
			assert.Equal(t, uint(1000001), p.Src, "packet Src should be preserved")
			assert.Equal(t, uint(100002), p.Repeater, "packet Repeater should be destination repeater ID")
			assert.True(t, p.GroupCall, "packet should be a group call")
		}

		// C3 should NOT receive anything (subscribed only to TG2)
		pkts3 := c3.Drain(drainWait)
		for _, p := range pkts3 {
			assert.NotEqual(t, uint(1), p.Dst, "C3 should not receive TG1 packets")
		}

		// C1 should NOT receive its own transmission back (non-simplex)
		pkts1 := c1.Drain(drainWait)
		for _, p := range pkts1 {
			assert.NotEqual(t, uint(1), p.Dst, "C1 should not echo its own TG1 call")
		}
	})
}

// TestGroupCallMMDVMToIPSC verifies that a group voice call originating from
// an MMDVM repeater is delivered to an IPSC server's connected peers via the
// hub's broadcast mechanism.
func TestGroupCallMMDVMToIPSC(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)

		stack.AssignTS1StaticTG(t, 100001, 1)

		stack.StartServers(t)

		// Connect MMDVM client
		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		defer c1.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))

		// Connect IPSC client
		ic1 := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, ic1.Connect(stack.IPSCAddr))
		defer ic1.Close()
		require.NoError(t, ic1.WaitReady(handshakeWait))

		time.Sleep(settleDuration)

		// MMDVM client sends group voice to TG1
		voicePkt := makeGroupVoicePacket(1000001, 1, 101, false)
		require.NoError(t, c1.SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makeGroupVoiceTermPacket(1000001, 1, 101, false)
		require.NoError(t, c1.SendDMRD(termPkt))

		// IPSC client should receive translated burst(s)
		bursts := ic1.Drain(drainWait)
		assert.NotEmpty(t, bursts, "IPSC client should receive broadcast of TG1 call from MMDVM")
	})
}

// TestGroupCallIPSCToMMDVM verifies that a group voice call from an IPSC peer
// flows through the translator, into the Hub, and out to subscribed MMDVM
// repeaters.
func TestGroupCallIPSCToMMDVM(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)

		stack.StartServers(t)

		// Connect MMDVM
		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		defer mc.Close()
		require.NoError(t, mc.WaitReady(handshakeWait))

		// Connect IPSC
		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, ic.Connect(stack.IPSCAddr))
		defer ic.Close()
		require.NoError(t, ic.WaitReady(handshakeWait))

		time.Sleep(settleDuration)

		// IPSC client sends group voice to TG1
		err := ic.SendGroupVoice(1000001, 1, false, 202)
		require.NoError(t, err)

		// MMDVM client should receive the translated DMRD packet
		pkts := mc.Drain(drainWait)
		assert.NotEmpty(t, pkts, "MMDVM client should receive TG1 voice from IPSC source")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst, "packet should target TG1")
			assert.True(t, p.GroupCall, "packet should be group call")
		}
	})
}

// TestPrivateCallToRepeater verifies that a private voice call addressed to a
// 6-digit repeater ID is delivered directly to that repeater and not to others.
func TestPrivateCallToRepeater(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)
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

		time.Sleep(settleDuration)

		// C1 sends private call to Repeater 100002
		voicePkt := makePrivateVoicePacket(100002, 303)
		require.NoError(t, c1.SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makePrivateVoiceTermPacket(100002, 303)
		require.NoError(t, c1.SendDMRD(termPkt))

		// C2 (repeater 100002) should receive the private call
		pkts2 := c2.Drain(drainWait)
		assert.NotEmpty(t, pkts2, "C2 should receive private call to its repeater ID")
		for _, p := range pkts2 {
			assert.Equal(t, uint(100002), p.Dst, "packet should be addressed to repeater 100002")
			assert.Equal(t, uint(1000001), p.Src, "packet Src should be preserved")
			assert.Equal(t, uint(100002), p.Repeater, "packet Repeater should be destination repeater ID")
			assert.False(t, p.GroupCall, "packet should be a private call")
		}

		// C1 should NOT get the private call back
		pkts1 := c1.Drain(drainWait)
		for _, p := range pkts1 {
			assert.NotEqual(t, uint(100002), p.Dst, "C1 should not receive the private call")
		}
	})
}

// TestPrivateCallToUserLastHeard verifies that a private call addressed to a
// 7-digit user ID is delivered to the repeater where the user was last heard.
func TestPrivateCallToUserLastHeard(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedUser(t, 1000002, "N1CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000002, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)
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

		time.Sleep(settleDuration)

		// First, simulate user 1000002 making a call on repeater 100002 so that
		// call tracking records their last-heard repeater.
		// The call must last >100ms or EndCall treats it as a key-up and deletes it.
		grpPkt := makeGroupVoicePacket(1000002, 1, 50, false)
		require.NoError(t, c2.SendDMRD(grpPkt))
		time.Sleep(200 * time.Millisecond)
		grpTerm := makeGroupVoiceTermPacket(1000002, 1, 50, false)
		require.NoError(t, c2.SendDMRD(grpTerm))

		// Wait for call tracking to persist
		time.Sleep(drainWait)
		// Drain any group call packets from both clients
		c1.Drain(500 * time.Millisecond)
		c2.Drain(500 * time.Millisecond)

		// Now C1 sends a private call to user 1000002
		privPkt := makePrivateVoicePacket(1000002, 404)
		require.NoError(t, c1.SendDMRD(privPkt))
		time.Sleep(50 * time.Millisecond)
		privTerm := makePrivateVoiceTermPacket(1000002, 404)
		require.NoError(t, c1.SendDMRD(privTerm))

		// C2 should receive the private call (user 1000002 was last heard on 100002)
		pkts2 := c2.Drain(drainWait)
		assert.NotEmpty(t, pkts2, "C2 should receive private call to user 1000002")
		for _, p := range pkts2 {
			assert.Equal(t, uint(1000002), p.Dst, "packet Dst should be user 1000002")
			assert.Equal(t, uint(1000001), p.Src, "packet Src should be preserved")
			assert.Equal(t, uint(100002), p.Repeater, "packet Repeater should be destination repeater ID")
			assert.False(t, p.GroupCall, "packet should be a private call")
		}
	})
}

// TestGroupCallMultipleTGSubscribers verifies that a group call on a talkgroup
// is delivered to ALL repeaters subscribed to that talkgroup.
func TestGroupCallMultipleTGSubscribers(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 5, "TG5")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100003, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100004, 1000001, testPassword)

		// All four repeaters subscribe to TG5 (on various slots)
		stack.AssignTS1StaticTG(t, 100001, 5)
		stack.AssignTS2StaticTG(t, 100002, 5)
		stack.AssignTS1StaticTG(t, 100003, 5)
		stack.AssignTS2StaticTG(t, 100004, 5)

		stack.StartServers(t)

		clients := make([]*testutils.MMDVMClient, 4)
		for i := uint32(0); i < 4; i++ {
			c := testutils.NewMMDVMClient(100001+i, "", testPassword)
			require.NoError(t, c.Connect(stack.MMDVMAddr))
			clients[i] = c
		}
		t.Cleanup(func() {
			for _, c := range clients {
				c.Close()
			}
		})
		for _, c := range clients {
			require.NoError(t, c.WaitReady(handshakeWait))
		}
		time.Sleep(settleDuration)

		// Client 0 (repeater 100001) sends group call to TG5
		voicePkt := makeGroupVoicePacket(1000001, 5, 505, false)
		require.NoError(t, clients[0].SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makeGroupVoiceTermPacket(1000001, 5, 505, false)
		require.NoError(t, clients[0].SendDMRD(termPkt))

		// Clients 1, 2, 3 should all receive the packet
		for i := uint(1); i < 4; i++ {
			expectedRepeater := 100001 + i
			pkts := clients[i].Drain(drainWait)
			assert.NotEmpty(t, pkts, "Client %d should receive TG5 packets", i)
			for _, p := range pkts {
				assert.Equal(t, uint(5), p.Dst, "Client %d: packet dst should be TG5", i)
				assert.Equal(t, uint(1000001), p.Src, "Client %d: packet Src should be preserved", i)
				assert.Equal(t, expectedRepeater, p.Repeater, "Client %d: packet Repeater should be destination", i)
			}
		}

		// Client 0 should NOT receive its own packets (non-simplex)
		pkts0 := clients[0].Drain(drainWait)
		for _, p := range pkts0 {
			assert.NotEqual(t, uint(5), p.Dst, "Source repeater should not echo TG5 to itself")
		}
	})
}

// TestNoDeliveryToNonexistentTG verifies that a group call to a talkgroup
// that does not exist in the DB is silently dropped — no repeater receives it.
func TestNoDeliveryToNonexistentTG(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)
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

		time.Sleep(settleDuration)

		// Send group call to TG 9999 which does not exist
		voicePkt := makeGroupVoicePacket(1000001, 9999, 606, false)
		require.NoError(t, c1.SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makeGroupVoiceTermPacket(1000001, 9999, 606, false)
		require.NoError(t, c1.SendDMRD(termPkt))

		// Neither client should receive anything for TG 9999
		pkts1 := c1.Drain(drainWait)
		pkts2 := c2.Drain(drainWait)
		for _, p := range pkts1 {
			assert.NotEqual(t, uint(9999), p.Dst, "C1 should not receive non-existent TG")
		}
		for _, p := range pkts2 {
			assert.NotEqual(t, uint(9999), p.Dst, "C2 should not receive non-existent TG")
		}
	})
}

// TestIPSCReceivesAllGroupCalls verifies that an IPSC server (registered with
// Broadcast: true) receives ALL group calls regardless of the specific talkgroup,
// whereas MMDVM repeaters receive only calls for subscribed talkgroups.
func TestIPSCReceivesAllGroupCalls(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedTalkgroup(t, 2, "TG2")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)

		// R1 only subscribes to TG1, R2 only to TG2
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS1StaticTG(t, 100002, 2)

		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))

		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, ic.Connect(stack.IPSCAddr))
		defer ic.Close()
		require.NoError(t, ic.WaitReady(handshakeWait))

		time.Sleep(settleDuration)

		// C1 sends a call to TG1
		require.NoError(t, c1.SendDMRD(makeGroupVoicePacket(1000001, 1, 701, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, c1.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 701, false)))

		time.Sleep(500 * time.Millisecond)

		// C2 sends a call to TG2
		require.NoError(t, c2.SendDMRD(makeGroupVoicePacket(1000001, 2, 702, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, c2.SendDMRD(makeGroupVoiceTermPacket(1000001, 2, 702, false)))

		// IPSC client should receive bursts from BOTH TG1 and TG2 (broadcast)
		bursts := ic.Drain(drainWait)
		assert.NotEmpty(t, bursts, "IPSC client should receive broadcast packets")

		// C2 should NOT receive TG1; C1 should NOT receive TG2
		pkts1 := c1.Drain(drainWait)
		pkts2 := c2.Drain(drainWait)
		for _, p := range pkts1 {
			assert.NotEqual(t, uint(2), p.Dst, "C1 should not receive TG2 packets")
		}
		for _, p := range pkts2 {
			assert.NotEqual(t, uint(1), p.Dst, "C2 should not receive TG1 packets")
		}
	})
}

// TestTS2StaticTGRouting verifies that a call on a TS2-subscribed talkgroup
// is delivered on TS2 (slot=true) to subscribers.
func TestTS2StaticTGRouting(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 10, "TG10")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)

		// R1 sends on TS2, R2 is subscribed to TG10 on TS2
		stack.AssignTS2StaticTG(t, 100001, 10)
		stack.AssignTS2StaticTG(t, 100002, 10)

		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))

		time.Sleep(settleDuration)

		// C1 sends group voice to TG10 on TS2 (slot=true)
		voicePkt := makeGroupVoicePacket(1000001, 10, 801, true)
		require.NoError(t, c1.SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makeGroupVoiceTermPacket(1000001, 10, 801, true)
		require.NoError(t, c1.SendDMRD(termPkt))

		// C2 should receive on TS2
		pkts := c2.Drain(drainWait)
		assert.NotEmpty(t, pkts, "C2 should receive TG10 packets on TS2")
		for _, p := range pkts {
			assert.Equal(t, uint(10), p.Dst, "packet should target TG10")
			assert.Equal(t, uint(1000001), p.Src, "packet Src should be preserved")
			assert.Equal(t, uint(100002), p.Repeater, "packet Repeater should be destination repeater ID")
			assert.True(t, p.Slot, "packet should be on TS2 (slot=true)")
			assert.True(t, p.GroupCall, "packet should be a group call")
		}
	})
}

// TestPrivateCallToRepeaterOwner verifies that a private call addressed to a
// user ID that owns a repeater is delivered to that repeater via last-heard lookup.
func TestPrivateCallToRepeaterOwner(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedUser(t, 1000002, "N1CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000002, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)
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

		time.Sleep(settleDuration)

		// First make user 1000002 active by having them transmit on repeater 100002.
		// The call must last >100ms or EndCall treats it as a key-up and deletes it.
		grpPkt := makeGroupVoicePacket(1000002, 1, 55, false)
		require.NoError(t, c2.SendDMRD(grpPkt))
		time.Sleep(200 * time.Millisecond)
		grpTerm := makeGroupVoiceTermPacket(1000002, 1, 55, false)
		require.NoError(t, c2.SendDMRD(grpTerm))

		// Wait for call tracking
		time.Sleep(drainWait)
		c1.Drain(500 * time.Millisecond)
		c2.Drain(500 * time.Millisecond)

		// Now send private call to user 1000002
		privPkt := makePrivateVoicePacket(1000002, 901)
		require.NoError(t, c1.SendDMRD(privPkt))
		time.Sleep(50 * time.Millisecond)
		privTerm := makePrivateVoiceTermPacket(1000002, 901)
		require.NoError(t, c1.SendDMRD(privTerm))

		// C2 should receive the private call
		pkts2 := c2.Drain(drainWait)
		assert.NotEmpty(t, pkts2, "C2 should receive private call to its owner (user 1000002)")
		for _, p := range pkts2 {
			assert.Equal(t, uint(1000002), p.Dst, "packet Dst should be user 1000002")
			assert.Equal(t, uint(1000001), p.Src, "packet Src should be preserved")
			assert.Equal(t, uint(100002), p.Repeater, "packet Repeater should be destination repeater ID")
			assert.False(t, p.GroupCall, "packet should be a private call")
		}
	})
}

// TestCrossProtocolPrivateCall verifies that a private call from MMDVM to a
// non-existent repeater ID doesn't panic and doesn't deliver anywhere.
func TestCrossProtocolPrivateCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)

		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		defer c1.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))

		time.Sleep(settleDuration)

		// C1 sends private call to a non-existent repeater ID
		privPkt := makePrivateVoicePacket(999999, 1001)
		require.NoError(t, c1.SendDMRD(privPkt))
		time.Sleep(50 * time.Millisecond)
		privTerm := makePrivateVoiceTermPacket(999999, 1001)
		require.NoError(t, c1.SendDMRD(privTerm))

		// Nothing should panic and no packets should be delivered anywhere unexpected
		pkts := c1.Drain(drainWait)
		for _, p := range pkts {
			assert.NotEqual(t, uint(999999), p.Dst, "no packets should arrive for non-existent repeater")
		}
	})
}

// TestRepeaterDoesNotEchoOwnGroupCall verifies that a repeater does NOT receive
// its own group call back (unless it's a simplex repeater, which we don't test here).
func TestRepeaterDoesNotEchoOwnGroupCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)

		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		defer c1.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))

		time.Sleep(settleDuration)

		// Send 5 voice packets and a terminator
		for i := uint(0); i < 5; i++ {
			pkt := makeGroupVoicePacket(1000001, 1, 1100, false)
			pkt.Seq = i
			pkt.DTypeOrVSeq = i % 6
			require.NoError(t, c1.SendDMRD(pkt))
			time.Sleep(20 * time.Millisecond)
		}
		term := makeGroupVoiceTermPacket(1000001, 1, 1100, false)
		require.NoError(t, c1.SendDMRD(term))

		// The source repeater should NOT receive any of these packets back
		pkts := c1.Drain(drainWait)
		for _, p := range pkts {
			if p.Dst == 1 && p.GroupCall {
				t.Errorf("source repeater echoed its own group call: %s", p.String())
			}
		}
	})
}

// makeGroupVoiceHeaderPacket builds a voice header (DataSync + VoiceHead) packet.
func makeGroupVoiceHeaderPacket(src, dst, streamID uint, slot bool) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         0,
		Src:         src,
		Dst:         dst,
		GroupCall:   true,
		Slot:        slot,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead),
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
}

// makePrivateVoiceHeaderPacket builds a private voice header packet.
func makePrivateVoiceHeaderPacket(src, dst, streamID uint, slot bool) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         0,
		Src:         src,
		Dst:         dst,
		GroupCall:   false,
		Slot:        slot,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead),
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
}

// TestParrotReturnsToSource verifies that a private call to 9990 (parrot)
// records the voice stream and plays it back to the source repeater only.
func TestParrotReturnsToSource(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)
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

		time.Sleep(settleDuration)

		const parrotUser = uint(9990)
		streamID := uint(8888)

		// Send voice header to parrot
		header := makePrivateVoiceHeaderPacket(1000001, parrotUser, streamID, false)
		require.NoError(t, c1.SendDMRD(header))
		time.Sleep(60 * time.Millisecond)

		// Send several voice frames
		for i := uint(0); i < 4; i++ {
			vPkt := makePrivateVoicePacket(parrotUser, streamID)
			vPkt.Seq = i + 1
			vPkt.DTypeOrVSeq = i % 6
			require.NoError(t, c1.SendDMRD(vPkt))
			time.Sleep(60 * time.Millisecond)
		}

		// Send voice terminator to trigger playback
		term := makePrivateVoiceTermPacket(parrotUser, streamID)
		require.NoError(t, c1.SendDMRD(term))

		// Parrot has a 3-second delay; wait up to 7 seconds for playback.
		pkts1 := c1.Drain(7 * time.Second)
		assert.NotEmpty(t, pkts1, "C1 should receive parrot playback packets")
		for _, p := range pkts1 {
			// Parrot swaps Src/Dst: original Src=1000001,Dst=9990 becomes Src=9990,Dst=1000001
			assert.Equal(t, uint(1000001), p.Dst, "parrot playback Dst should be original caller")
			assert.Equal(t, parrotUser, p.Src, "parrot playback Src should be 9990")
			assert.Equal(t, uint(100001), p.Repeater, "parrot playback Repeater should be source repeater")
			assert.False(t, p.GroupCall, "parrot playback should be private call")
		}

		// C2 should NOT receive any parrot playback
		pkts2 := c2.Drain(1 * time.Second)
		for _, p := range pkts2 {
			assert.NotEqual(t, parrotUser, p.Dst,
				"C2 should not receive parrot playback meant for C1")
		}
	})
}

// TestUnlinkRemovesDynamicTG verifies that a private call to 4000 (unlink)
// removes the dynamic talkgroup link for the sending repeater's timeslot,
// so it no longer receives calls on that TG.
func TestUnlinkRemovesDynamicTG(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 5, "TG5")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)

		// Only R2 has TG5 as a static TG so it always receives.
		// R1 has NO static TG5 — it will only receive TG5 via dynamic linking.
		stack.AssignTS1StaticTG(t, 100002, 5)

		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))

		time.Sleep(settleDuration)

		// Step 1: C1 sends group voice to TG5 on TS1. This dynamically links R1 to TG5.
		header1 := makeGroupVoiceHeaderPacket(1000001, 5, 2001, false)
		require.NoError(t, c1.SendDMRD(header1))
		time.Sleep(60 * time.Millisecond)
		voice1 := makeGroupVoicePacket(1000001, 5, 2001, false)
		require.NoError(t, c1.SendDMRD(voice1))
		time.Sleep(60 * time.Millisecond)
		term1 := makeGroupVoiceTermPacket(1000001, 5, 2001, false)
		require.NoError(t, c1.SendDMRD(term1))

		// Wait for dynamic linking to complete
		time.Sleep(settleDuration)
		c1.Drain(500 * time.Millisecond)
		c2.Drain(500 * time.Millisecond)

		// Step 2: C2 sends group voice to TG5 — C1 should receive (dynamically linked).
		header2 := makeGroupVoiceHeaderPacket(1000001, 5, 2002, false)
		require.NoError(t, c2.SendDMRD(header2))
		time.Sleep(60 * time.Millisecond)
		voice2 := makeGroupVoicePacket(1000001, 5, 2002, false)
		require.NoError(t, c2.SendDMRD(voice2))
		time.Sleep(60 * time.Millisecond)
		term2 := makeGroupVoiceTermPacket(1000001, 5, 2002, false)
		require.NoError(t, c2.SendDMRD(term2))

		pkts := c1.Drain(drainWait)
		require.NotEmpty(t, pkts, "C1 should receive TG5 call (dynamically linked)")
		for _, p := range pkts {
			assert.Equal(t, uint(5), p.Dst, "C1: packet should be TG5")
		}

		// Step 3: C1 sends unlink (private call to 4000) on TS1.
		unlinkHeader := makePrivateVoiceHeaderPacket(1000001, 4000, 3001, false)
		require.NoError(t, c1.SendDMRD(unlinkHeader))
		time.Sleep(60 * time.Millisecond)
		unlinkTerm := makePrivateVoiceTermPacket(4000, 3001)
		require.NoError(t, c1.SendDMRD(unlinkTerm))

		// Wait for unlink to process
		time.Sleep(settleDuration)
		c1.Drain(500 * time.Millisecond)
		c2.Drain(500 * time.Millisecond)

		// Step 4: C2 sends another group voice to TG5 — C1 should NOT receive it.
		header3 := makeGroupVoiceHeaderPacket(1000001, 5, 2003, false)
		require.NoError(t, c2.SendDMRD(header3))
		time.Sleep(60 * time.Millisecond)
		voice3 := makeGroupVoicePacket(1000001, 5, 2003, false)
		require.NoError(t, c2.SendDMRD(voice3))
		time.Sleep(60 * time.Millisecond)
		term3 := makeGroupVoiceTermPacket(1000001, 5, 2003, false)
		require.NoError(t, c2.SendDMRD(term3))

		pktsAfterUnlink := c1.Drain(drainWait)
		for _, p := range pktsAfterUnlink {
			assert.NotEqual(t, uint(5), p.Dst,
				"C1 should NOT receive TG5 after unlinking: %s", p.String())
		}

		// C2 should still receive its own TG5 (static), but verify no unlink leak.
		// Actually C2 won't echo its own packets (non-simplex), so just drain.
		c2.Drain(500 * time.Millisecond)
	})
}

// TestMultiReplicaDelivery verifies that when two replicas of the app share the
// same pubsub/KV/DB, a group call is delivered ONLY by the replica that holds
// the repeater's UDP session — not by the other replica.
//
// This catches the production bug where replica B won a lease and sent packets
// from a different UDP socket, causing the destination repeater to ignore them.
func TestMultiReplicaDelivery(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)

		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS1StaticTG(t, 100002, 1)

		// Spawn a second replica (separate Hub + MMDVM server) sharing the same
		// DB, pubsub, and KV. This simulates a second pod in a multi-replica deploy.
		// The second replica also subscribes to all pubsub topics via Hub.Start().
		_ = stack.SpawnSecondReplica(t)

		stack.StartServers(t)

		// Connect both repeaters ONLY to the primary replica's MMDVM server.
		// This mirrors production where all repeaters happen to connect to one pod.
		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))

		time.Sleep(settleDuration)

		// C1 sends group voice to TG1
		voicePkt := makeGroupVoicePacket(1000001, 1, 5001, false)
		require.NoError(t, c1.SendDMRD(voicePkt))
		time.Sleep(50 * time.Millisecond)
		termPkt := makeGroupVoiceTermPacket(1000001, 1, 5001, false)
		require.NoError(t, c1.SendDMRD(termPkt))

		// C2 should still receive the call despite a second replica also consuming
		// pubsub messages. Before the fix, the second replica could win a lease and
		// send from the wrong socket, leaving C2 with no data.
		pkts := c2.Drain(drainWait)
		assert.NotEmpty(t, pkts, "C2 should receive TG1 call even with a second replica running")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst, "packet should target TG1")
			assert.Equal(t, uint(1000001), p.Src, "packet Src should be preserved")
			assert.Equal(t, uint(100002), p.Repeater, "packet Repeater should be destination repeater ID")
			assert.True(t, p.GroupCall, "packet should be a group call")
		}

		// C1 should NOT echo its own call
		pkts1 := c1.Drain(drainWait)
		for _, p := range pkts1 {
			assert.NotEqual(t, uint(1), p.Dst, "source repeater should not echo its own call")
		}
	})
}
