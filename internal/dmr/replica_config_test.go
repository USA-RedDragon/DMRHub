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
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sendGroupCall(t *testing.T, c *testutils.MMDVMClient, dst, stream uint, bursts int) {
	t.Helper()
	for i := range bursts {
		pkt := makeGroupVoicePacket(1000001, dst, stream, false)
		pkt.Seq = uint(i)
		require.NoError(t, c.SendDMRD(pkt))
		time.Sleep(60 * time.Millisecond)
	}
	term := makeGroupVoiceTermPacket(1000001, dst, stream, false)
	term.Seq = uint(bursts)
	require.NoError(t, c.SendDMRD(term))
}

func countDst(pkts []models.Packet, dst uint) int {
	n := 0
	for _, p := range pkts {
		if p.GroupCall && p.Dst == dst {
			n++
		}
	}
	return n
}

func TestStaticTalkgroupEditOnOneReplicaRoutesOnAnother(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedTalkgroup(t, 2, "TG2")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)

		replicaB := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(replicaB.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		sendGroupCall(t, c1, 1, 7001, 4)
		require.Positive(t, countDst(c2.Drain(drainWait), 1), "TG1 must reach replica B before the edit")

		rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002}}
		require.NoError(t, stack.DB.Model(&rpt).Association("TS1StaticTalkgroups").Replace([]models.Talkgroup{{ID: 2}}))
		stack.Hub.ReloadRepeater(context.Background(), 100002)

		sendGroupCall(t, c1, 1, 7002, 8)
		assert.Zero(t, countDst(c2.Drain(drainWait), 1), "TG1 was removed on replica A and must stop on replica B")

		sendGroupCall(t, c1, 2, 7003, 8)
		assert.Positive(t, countDst(c2.Drain(drainWait), 2), "TG2 was added on replica A and must reach replica B")
	})
}
