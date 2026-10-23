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
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fbpStack struct {
	mc      *testutils.MMDVMClient
	a, b, c *testutils.OpenBridgeClient
}

// startFBPStack starts a repeater on TG 1 and three OpenBridge peers: a and
// b enhanced, c version 1. a takes TG 1 in; all three take calls from
// 1000001 out.
func startFBPStack(t *testing.T, stack *testutils.IntegrationStack) fbpStack {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	stack.AssignTS1StaticTG(t, 100001, 1)
	stack.StartServers(t)

	peer := func(id uint, enhanced bool) *testutils.OpenBridgeClient {
		c := testutils.NewOpenBridgeClient(uint32(id), obPassword)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.OpenBridgeAddr))
		addr := c.LocalAddr()
		stack.SeedPeer(t, id, 1000001, obPassword, addr.IP.String(), addr.Port, true, true)
		require.NoError(t, stack.DB.Model(&models.Peer{}).Where("id = ?", id).Update("enhanced", enhanced).Error)
		stack.SeedPeerRule(t, id, true, 1, 1)
		stack.SeedPeerRule(t, id, false, 1000001, 1000001)
		return c
	}
	s := fbpStack{a: peer(500001, true), b: peer(500002, true), c: peer(500003, false)}
	s.mc = testutils.NewMMDVMClient(100001, "", testPassword)
	t.Cleanup(s.mc.Close)
	require.NoError(t, s.mc.Connect(stack.MMDVMAddr))
	require.NoError(t, s.mc.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	return s
}

func dataOf(bs []testutils.ReceivedBridge, stream uint32) []testutils.ReceivedBridge {
	var out []testutils.ReceivedBridge
	for _, b := range bs {
		if b.Data.Stream == stream {
			out = append(out, b)
		}
	}
	return out
}

// TestEnhancedOpenBridgePeersGetKeepalives checks that an enhanced peer
// gets a BCKA, as FreeDMR's send_bcka sends every 10 s, and a version 1
// peer gets none.
func TestEnhancedOpenBridgePeersGetKeepalives(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startFBPStack(t, stack)
		var bcka int
		for _, c := range s.a.DrainControls(2500 * time.Millisecond) {
			if c.Command == dmrd.CommandBCKA {
				bcka++
			}
		}
		assert.Positive(t, bcka, "the enhanced peer gets a BCKA")
		assert.Empty(t, s.c.DrainControls(100*time.Millisecond), "a version 1 peer gets no control packet")
	})
}

// TestEnhancedOpenBridgeCarriesTheOrigin sends a call from a local
// repeater and a DMRE call from an enhanced peer. Enhanced peers get DMRE
// version 5 with the source server and repeater kept and one more hop, as
// FreeDMR's hblink.py builds it; the version 1 peer gets version 1.
func TestEnhancedOpenBridgeCarriesTheOrigin(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startFBPStack(t, stack)

		require.NoError(t, s.mc.SendDMRD(makeGroupVoicePacket(1000001, 1, 9601, false)))
		got := dataOf(s.a.DrainBridges(drainWait), 9601)
		if assert.Len(t, got, 1, "the enhanced peer gets the local call") {
			assert.Equal(t, uint8(5), got[0].Version)
			assert.Equal(t, uint8(1), got[0].Hops)
			assert.Equal(t, uint32(500001), got[0].SourceServer, "with no server ID, the network ID of the link")
			assert.Equal(t, uint32(100001), got[0].SourceRepeater)
			assert.Equal(t, uint32(500001), got[0].Data.Repeater)
			assert.InDelta(t, float64(time.Now().UnixNano()), float64(got[0].Time), float64(5*time.Second))
		}
		v1 := dataOf(s.c.DrainBridges(drainWait), 9601)
		if assert.Len(t, v1, 1, "the version 1 peer gets the local call") {
			assert.Equal(t, uint8(1), v1[0].Version)
		}

		burst := makeBurst(1000001, 1, 9602, true, dmrconst.FrameDataSync, uint(enums.DataTypeCSBK), 0)
		burst.Repeater = 500001
		require.NoError(t, s.a.SendBridge(dmrd.Bridge{Version: 5, Data: burst.Data(), SourceServer: 3129, SourceRepeater: 312910, Hops: 3}))
		fwd := dataOf(s.b.DrainBridges(drainWait), 9602)
		if assert.Len(t, fwd, 1, "the other enhanced peer gets the call") {
			assert.Equal(t, uint8(5), fwd[0].Version)
			assert.Equal(t, uint8(4), fwd[0].Hops, "one more hop")
			assert.Equal(t, uint32(3129), fwd[0].SourceServer, "the source server is kept")
			assert.Equal(t, uint32(312910), fwd[0].SourceRepeater, "the source repeater is kept")
			assert.Equal(t, burst.DMRData, [33]byte(fwd[0].Data.Burst))
		}
		v1 = dataOf(s.c.DrainBridges(drainWait), 9602)
		if assert.Len(t, v1, 1, "the version 1 peer gets the call") {
			assert.Equal(t, uint8(1), v1[0].Version)
		}
		assert.Len(t, s.mc.Drain(drainWait), 1, "the local repeater gets the call")
	})
}

// TestEnhancedOpenBridgeHopLimit sends a DMRE call that already crossed
// FreeDMR's MAX_FBP_HOPS of 20 links. Nobody gets it, and the sender gets a
// BCSQ for the stream, as FreeDMR does.
func TestEnhancedOpenBridgeHopLimit(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startFBPStack(t, stack)
		s.a.DrainControls(1500 * time.Millisecond)

		burst := makeBurst(1000001, 1, 9603, true, dmrconst.FrameDataSync, uint(enums.DataTypeCSBK), 0)
		burst.Repeater = 500001
		require.NoError(t, s.a.SendBridge(dmrd.Bridge{Version: 5, Data: burst.Data(), SourceServer: 3129, Hops: 20}))
		assert.Empty(t, dataOf(s.b.DrainBridges(drainWait), 9603))
		assert.Empty(t, dataOf(s.c.DrainBridges(100*time.Millisecond), 9603))
		assert.Empty(t, s.mc.Drain(100*time.Millisecond))
		var quench bool
		for _, c := range s.a.DrainControls(time.Second) {
			if c.Command == dmrd.CommandBCSQ && c.Stream == 9603 && c.TalkGroup == 1 {
				quench = true
			}
		}
		assert.True(t, quench, "the sender gets a BCSQ for the stream")
	})
}
