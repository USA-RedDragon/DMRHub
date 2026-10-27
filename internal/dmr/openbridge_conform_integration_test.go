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
