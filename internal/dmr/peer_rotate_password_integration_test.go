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
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rotatedPeerPassword = "n3w-Peer_Pass"

// TestRotatedPeerPasswordStopsAtOnceOnEveryReplica rotates the password of
// a peer that sends to both replicas, through the hub of the first, right
// after both loaded the peer, so that no expiry of their peer tables hides
// a missed change. From then on neither replica may take a packet signed
// with the old password, and both must take one signed with the new
// password.
func TestRotatedPeerPasswordStopsAtOnceOnEveryReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const peerID = 500001
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)
		stack.SeedPeer(t, peerID, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, peerID, true, 1, 1)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))

		client := func(password, addr string) *testutils.OpenBridgeClient {
			c := testutils.NewOpenBridgeClient(peerID, password)
			t.Cleanup(c.Close)
			require.NoError(t, c.Connect(addr))
			return c
		}
		replicas := []struct {
			name string
			addr string
		}{{"first", stack.OpenBridgeAddr}, {"second", second.OpenBridgeAddr}}
		old := make([]*testutils.OpenBridgeClient, len(replicas))
		for i, r := range replicas {
			old[i] = client(obPassword, r.addr)
		}
		time.Sleep(settleDuration)
		stream := uint32(9701)
		for i, r := range replicas {
			require.NoError(t, old[i].SendGroupVoice(1000001, 1, stream))
			require.Len(t, mc.Drain(drainWait), 3, "the %s replica takes the old password before the rotation", r.name)
			stream++
		}

		stack.Hub.PeerChanged(peerID)
		time.Sleep(settleDuration)
		require.NoError(t, models.SetPeerPassword(stack.DB, peerID, rotatedPeerPassword))
		stack.Hub.PeerChanged(peerID)
		time.Sleep(settleDuration)

		streams := make([]uint32, len(replicas))
		for i := range replicas {
			streams[i] = stream
			require.NoError(t, old[i].SendGroupVoice(1000001, 1, stream))
			stream++
		}
		got := map[uint]int{}
		for _, p := range mc.Drain(drainWait) {
			got[p.StreamID]++
		}
		for i, r := range replicas {
			assert.Zero(t, got[uint(streams[i])], "the %s replica must refuse the old password", r.name)
		}
		for _, r := range replicas {
			require.NoError(t, client(rotatedPeerPassword, r.addr).SendGroupVoice(1000001, 1, stream))
			assert.Len(t, mc.Drain(drainWait), 3, "the %s replica must take the new password", r.name)
			stream++
		}
	})
}

// TestOpenBridgeForgetsTheVersionOfARotatedPeer moves a version 1 link up
// with a BCVE, then rotates the password of the peer. The other server
// starts again with the new password, so the hub must send version 1 again
// until it names a newer one.
func TestOpenBridgeForgetsTheVersionOfARotatedPeer(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startConformStack(t, stack, false)
		s.moveUp(t, 9711)

		fresh := testutils.NewOpenBridgeClient(conformPeer, rotatedPeerPassword)
		t.Cleanup(fresh.Close)
		require.NoError(t, fresh.Connect(stack.OpenBridgeAddr))
		addr := fresh.LocalAddr()
		require.NoError(t, stack.DB.Model(&models.Peer{}).Where("id = ?", conformPeer).UpdateColumns(map[string]any{"ip": addr.IP.String(), "port": addr.Port}).Error)
		require.NoError(t, models.SetPeerPassword(stack.DB, conformPeer, rotatedPeerPassword))
		stack.Hub.PeerChanged(conformPeer)
		time.Sleep(200 * time.Millisecond)

		const stream = 9712
		require.NoError(t, s.mc.SendDMRD(makeGroupVoicePacket(conformUser, 1, stream, false)))
		got := dataOf(fresh.DrainBridges(drainWait), stream)
		if assert.Len(t, got, 1) {
			assert.Equal(t, uint8(1), got[0].Version, "a rotation of the password keeps the version that the peer named before it")
		}
	})
}
