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

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type enhancedStack struct {
	mc   *testutils.MMDVMClient
	peer *testutils.OpenBridgeClient
}

func startEnhancedStack(t *testing.T, stack *testutils.IntegrationStack) enhancedStack {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	stack.AssignTS1StaticTG(t, 100001, 1)
	stack.StartServers(t)

	peer := testutils.NewOpenBridgeClient(500001, obPassword)
	t.Cleanup(peer.Close)
	require.NoError(t, peer.Connect(stack.OpenBridgeAddr))
	addr := peer.LocalAddr()
	stack.SeedPeer(t, 500001, 1000001, obPassword, addr.IP.String(), addr.Port, true, true)
	stack.SeedPeerRule(t, 500001, true, 1, 1)
	stack.SeedPeerRule(t, 500001, false, 1000001, 1000001)

	mc := testutils.NewMMDVMClient(100001, "", testPassword)
	t.Cleanup(mc.Close)
	require.NoError(t, mc.Connect(stack.MMDVMAddr))
	require.NoError(t, mc.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	return enhancedStack{mc: mc, peer: peer}
}

// TestOpenBridgeTakesEnhancedPackets sends DMRE packets of versions 4 and 5
// and the control packets of FreeDMR's enhanced OpenBridge. The DMRE bursts
// must reach the repeater unchanged; the control packets route nothing.
func TestOpenBridgeTakesEnhancedPackets(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startEnhancedStack(t, stack)

		for _, c := range []dmrd.BridgeControl{
			{Command: dmrd.CommandBCKA},
			{Command: dmrd.CommandBCVE, Version: 1},
			{Command: dmrd.CommandBCST},
		} {
			require.NoError(t, s.peer.SendControl(c))
		}
		assert.Empty(t, s.mc.Drain(300*time.Millisecond), "control packets must route nothing")

		for i, version := range []uint8{4, 5} {
			sent := makeBurst(1000001, 1, uint(9300+i), true, dmrconst.FrameDataSync, uint(enums.DataTypeCSBK), 0)
			sent.Repeater = 500001
			require.NoError(t, s.peer.SendEnhancedPacket(sent, version))
			select {
			case got := <-s.mc.ReceivedPackets():
				assert.Equal(t, sent.DMRData, got.DMRData, "version %d burst", version)
				assert.Equal(t, sent.StreamID, got.StreamID, "version %d stream", version)
			case <-time.After(drainWait):
				t.Fatalf("the version %d DMRE packet did not reach the repeater", version)
			}
		}
	})
}

// TestOpenBridgeSourceQuench sends a BCSQ for one stream. The hub must stop
// sending that stream to the peer, and keep sending other streams.
func TestOpenBridgeSourceQuench(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startEnhancedStack(t, stack)

		require.NoError(t, s.mc.SendDMRD(makeGroupVoicePacket(1000001, 1, 9401, false)))
		require.Len(t, s.peer.Drain(drainWait), 1, "the peer gets the stream before the quench")

		require.NoError(t, s.peer.SendControl(dmrd.BridgeControl{Command: dmrd.CommandBCSQ, TalkGroup: 1, Stream: 9401}))
		time.Sleep(200 * time.Millisecond)
		require.NoError(t, s.mc.SendDMRD(makeGroupVoicePacket(1000001, 1, 9401, false)))
		assert.Empty(t, s.peer.Drain(drainWait), "the quenched stream must not go to the peer")

		require.NoError(t, s.mc.SendDMRD(makeGroupVoicePacket(1000001, 1, 9402, false)))
		assert.Len(t, s.peer.Drain(drainWait), 1, "another stream still goes to the peer")
	})
}
