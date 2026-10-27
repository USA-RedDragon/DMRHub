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
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestOpenBridgeRecordsTheLastPingOfAPeer sends calls from a peer to each
// replica in turn. Each replica must record the last ping of the peer in
// the database that both share, at most once per 30 s, so that two calls
// of three packets each make one write per replica and not one per
// packet. The write must not move the generation of the peer, which
// would make the replicas forget the link version.
func TestOpenBridgeRecordsTheLastPingOfAPeer(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const peerID = 500001
		var counting atomic.Bool
		var writes atomic.Int32
		require.NoError(t, stack.DB.Callback().Update().After("gorm:update").Register("test:count_peer_writes", func(tx *gorm.DB) {
			if counting.Load() && tx.Statement.Table == "peers" {
				writes.Add(1)
			}
		}))
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

		peer := func() models.Peer {
			p, err := models.FindPeerByID(stack.DB, peerID)
			require.NoError(t, err)
			return p
		}
		before := peer()
		counting.Store(true)
		require.True(t, before.LastPing.IsZero() || before.LastPing.Year() <= 1, "a new peer has no last ping")

		stream := uint32(9801)
		for i, addr := range []string{stack.OpenBridgeAddr, second.OpenBridgeAddr} {
			c := testutils.NewOpenBridgeClient(peerID, obPassword)
			t.Cleanup(c.Close)
			require.NoError(t, c.Connect(addr))
			time.Sleep(settleDuration)
			sent := time.Now()
			for range 2 {
				require.NoError(t, c.SendGroupVoice(1000001, 1, stream))
				stream++
			}
			require.Len(t, mc.Drain(drainWait), 6, "replica %d takes the calls", i+1)
			want := int32(i + 1)
			require.Eventually(t, func() bool { return writes.Load() == want }, 2*time.Second, 10*time.Millisecond,
				"replica %d records the last ping once", i+1)
			got := peer()
			assert.False(t, got.LastPing.Before(sent.Truncate(time.Second)), "replica %d records a recent last ping, got %v", i+1, got.LastPing)
			assert.Equal(t, before.UpdatedAt.UnixNano(), got.UpdatedAt.UnixNano(), "the last ping keeps the generation of the peer")
		}
		time.Sleep(settleDuration)
		assert.Equal(t, int32(2), writes.Load(), "no write per packet")
	})
}
