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

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gatewayOptions = "StartRef=4013;RelinkTime=15;UserLink=1;TS1_1=262;"

// TestSessionPeerLoginAndVoice logs in a DMRGateway-like peer with and
// without RPTO options, and passes a group call from it to a repeater and
// back.
func TestSessionPeerLoginAndVoice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		options string
	}{
		{name: "no options"},
		{name: "options", options: gatewayOptions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
				stack.SeedUser(t, 1000001, "N0CALL")
				stack.SeedTalkgroup(t, 1, "TG1")
				stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
				stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
				stack.AssignTS1StaticTG(t, 100001, 1)
				stack.AssignTS1StaticTG(t, 100002, 1)
				stack.StartServers(t)

				gw, err := testutils.NewSessionPeer(100001, testPassword, tc.options)
				require.NoError(t, err)
				require.NoError(t, gw.Connect(stack.MMDVMAddr))
				defer gw.Close()
				require.NoError(t, gw.WaitReady(handshakeWait))

				rpt := testutils.NewMMDVMClient(100002, "", testPassword)
				require.NoError(t, rpt.Connect(stack.MMDVMAddr))
				defer rpt.Close()
				require.NoError(t, rpt.WaitReady(handshakeWait))
				time.Sleep(settleDuration)

				require.NoError(t, gw.SendDMRD(makeGroupVoicePacket(1000001, 1, 7001, false)))
				time.Sleep(50 * time.Millisecond)
				require.NoError(t, gw.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7001, false)))
				pkts := rpt.Drain(drainWait)
				require.NotEmpty(t, pkts, "repeater should receive the call of the peer")
				for _, p := range pkts {
					assert.Equal(t, uint(1), p.Dst)
					assert.Equal(t, uint(100002), p.Repeater)
				}

				time.Sleep(callHoldTime)
				require.NoError(t, rpt.SendDMRD(makeGroupVoicePacket(1000001, 1, 7002, false)))
				time.Sleep(50 * time.Millisecond)
				require.NoError(t, rpt.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7002, false)))
				pkts = gw.Drain(drainWait)
				require.NotEmpty(t, pkts, "peer should receive the call of the repeater")
				for _, p := range pkts {
					assert.Equal(t, uint(1), p.Dst)
					assert.Equal(t, uint(100001), p.Repeater)
				}

				assert.Equal(t, session.StateRunning, gw.State())
				assert.Zero(t, gw.Downs(), "the session should not have ended")
				rec, err := servers.MakeKVClient(stack.KV).GetRepeater(context.Background(), 100001)
				require.NoError(t, err)
				assert.NotZero(t, rec.PingsReceived, "the pings of the peer should be recorded")
			})
		})
	}
}

// TestSessionPeerMovesToSecondReplica logs a peer in to one replica and
// then sends its traffic to another, as a load balancer does in a rolling
// restart. The second replica must take the session over without a new
// login.
func TestSessionPeerMovesToSecondReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS1StaticTG(t, 100002, 1)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		gw, err := testutils.NewSessionPeer(100001, testPassword, gatewayOptions)
		require.NoError(t, err)
		require.NoError(t, gw.Connect(stack.MMDVMAddr))
		defer gw.Close()
		require.NoError(t, gw.WaitReady(handshakeWait))

		rpt := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, rpt.Connect(stack.MMDVMAddr))
		defer rpt.Close()
		require.NoError(t, rpt.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		require.NoError(t, gw.Retarget(second))
		time.Sleep(settleDuration)

		require.NoError(t, gw.SendDMRD(makeGroupVoicePacket(1000001, 1, 7101, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, gw.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7101, false)))
		pkts := rpt.Drain(drainWait)
		assert.NotEmpty(t, pkts, "repeater should receive the call that the second replica took")
		assert.Equal(t, session.StateRunning, gw.State())
		assert.Zero(t, gw.Downs(), "the second replica should not refuse the session")
	})
}
