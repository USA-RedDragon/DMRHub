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
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type replicaOB struct {
	stack    *testutils.IntegrationStack
	second   testutils.Replica
	peer     *testutils.OpenBridgeClient
	on1, on2 *testutils.MMDVMClient
}

// startReplicaOB runs two replicas with a repeater on TG 1 on each and an
// OpenBridge peer that takes calls from 1000001.
func startReplicaOB(t *testing.T, stack *testutils.IntegrationStack) replicaOB {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	stack.SeedMMDVMRepeater(t, 100003, 1000001, testPassword)
	stack.AssignTS1StaticTG(t, 100001, 1)
	stack.AssignTS1StaticTG(t, 100003, 1)
	r := replicaOB{stack: stack, second: stack.SpawnSecondReplica(t)}
	stack.StartServers(t)

	r.peer = testutils.NewOpenBridgeClient(500002, obPassword)
	t.Cleanup(r.peer.Close)
	require.NoError(t, r.peer.Connect(stack.OpenBridgeAddr))
	addr := r.peer.LocalAddr()
	stack.SeedPeer(t, 500002, 1000001, obPassword, addr.IP.String(), addr.Port, true, true)
	stack.SeedPeerRule(t, 500002, false, 1000001, 1000001)

	client := func(id uint32, at string) *testutils.MMDVMClient {
		c := testutils.NewMMDVMClient(id, "", testPassword)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(at))
		require.NoError(t, c.WaitReady(handshakeWait))
		return c
	}
	r.on1, r.on2 = client(100001, stack.MMDVMAddr), client(100003, r.second.MMDVMAddr)
	time.Sleep(settleDuration)
	return r
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	n, err := strconv.Atoi(p)
	require.NoError(t, err)
	return n
}

// leader returns the server that holds the egress lease, its address and
// the address of the other one.
func (r replicaOB) leader(t *testing.T) (*openbridge.Server, string, string) {
	t.Helper()
	var l *openbridge.Server
	var la, oa string
	require.Eventually(t, func() bool {
		one, two := r.stack.OpenBridgeServer.IsEgressLeader(), r.second.OpenBridgeServer.IsEgressLeader()
		switch {
		case one && !two:
			l, la, oa = r.stack.OpenBridgeServer, r.stack.OpenBridgeAddr, r.second.OpenBridgeAddr
		case two && !one:
			l, la, oa = r.second.OpenBridgeServer, r.second.OpenBridgeAddr, r.stack.OpenBridgeAddr
		default:
			return false
		}
		return true
	}, 2*openbridge.EgressLeaseTTL, 50*time.Millisecond, "exactly one replica holds the egress lease")
	return l, la, oa
}

// TestOpenBridgeEgressFromOneReplica checks that a peer gets each call
// once, from the replica that holds the egress lease, whichever replica
// routed it, and that the other replica takes over within the lease time
// when the holder stops.
func TestOpenBridgeEgressFromOneReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startReplicaOB(t, stack)
		leader, leaderAddr, otherAddr := r.leader(t)

		stream := uint32(9700)
		for _, c := range []*testutils.MMDVMClient{r.on1, r.on2} {
			stream++
			require.NoError(t, c.SendDMRD(makeGroupVoicePacket(1000001, 1, uint(stream), false)))
			got := dataOf(r.peer.DrainBridges(drainWait), stream)
			if assert.Len(t, got, 1, "the peer gets the call once") {
				assert.Equal(t, portOf(t, leaderAddr), got[0].From.Port, "from the replica that holds the lease")
			}
		}

		var survivor *testutils.MMDVMClient
		if leader == r.stack.OpenBridgeServer {
			survivor = r.on2
		} else {
			survivor = r.on1
		}
		start := time.Now()
		require.NoError(t, leader.Stop(t.Context()))
		var from int
		for time.Since(start) < 3*openbridge.EgressLeaseTTL {
			stream++
			require.NoError(t, survivor.SendDMRD(makeGroupVoicePacket(1000001, 1, uint(stream), false)))
			if got := dataOf(r.peer.DrainBridges(100*time.Millisecond), stream); len(got) > 0 {
				from = got[0].From.Port
				break
			}
		}
		gap := time.Since(start)
		t.Logf("egress moved to the other replica after %s", gap.Round(time.Millisecond))
		assert.Equal(t, portOf(t, otherAddr), from, "the other replica sends after the holder stops")
		assert.Less(t, gap, openbridge.EgressLeaseTTL, "no traffic waits longer than the lease")
	})
}

// TestOpenBridgeQuenchAcrossReplicas sends a BCSQ to the replica that does
// not hold the egress lease. The holder must not send that stream to the
// peer, and must send other streams.
func TestOpenBridgeQuenchAcrossReplicas(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startReplicaOB(t, stack)
		leader, _, otherAddr := r.leader(t)

		require.NoError(t, r.on1.SendDMRD(makeGroupVoicePacket(1000001, 1, 9801, false)))
		require.Len(t, dataOf(r.peer.DrainBridges(drainWait), 9801), 1, "the peer gets the stream before the quench")

		require.NoError(t, r.peer.SendControlTo(otherAddr, dmrd.BridgeControl{Command: dmrd.CommandBCSQ, TalkGroup: 1, Stream: 9801}))
		require.Eventually(t, func() bool { return leader.IsQuenched(500002, 9801) }, handshakeWait, 10*time.Millisecond, "the holder of the lease did not get the quench")
		for _, c := range []*testutils.MMDVMClient{r.on1, r.on2} {
			require.NoError(t, c.SendDMRD(makeGroupVoicePacket(1000001, 1, 9801, false)))
		}
		assert.Empty(t, dataOf(r.peer.DrainBridges(drainWait), 9801), "the quenched stream must not go to the peer")

		require.NoError(t, r.on2.SendDMRD(makeGroupVoicePacket(1000001, 1, 9802, false)))
		assert.Len(t, dataOf(r.peer.DrainBridges(drainWait), 9802), 1, "another stream still goes to the peer")
	})
}

// TestOpenBridgeEgressHandoverLosesNothing stops the replica that holds
// the egress lease, as a rolling restart does, in the middle of a call that
// the other replica routes. The peer must get every burst of the call
// once: the holder hands the lease over without a gap.
func TestOpenBridgeEgressHandoverLosesNothing(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startReplicaOB(t, stack)
		leader, _, _ := r.leader(t)
		talker := r.on1
		if leader == r.stack.OpenBridgeServer {
			talker = r.on2
		}

		const bursts = 60
		const stream = 9800
		stopped := make(chan struct{})
		for i := range bursts {
			p := makeGroupVoicePacket(1000001, 1, stream, false)
			p.Seq = uint(i)
			require.NoError(t, talker.SendDMRD(p))
			if i == bursts/3 {
				go func() {
					defer close(stopped)
					_ = leader.Stop(servers.WithGracefulHandoff(t.Context(), true))
				}()
			}
			time.Sleep(30 * time.Millisecond)
		}
		<-stopped
		seen := map[uint8]int{}
		for _, b := range dataOf(r.peer.DrainBridges(drainWait), stream) {
			seen[b.Data.Seq]++
		}
		var lost, twice []int
		for i := range bursts {
			switch seen[uint8(i)] {
			case 0:
				lost = append(lost, i)
			case 1:
			default:
				twice = append(twice, i)
			}
		}
		assert.Empty(t, lost, "bursts the peer never got")
		assert.Empty(t, twice, "bursts the peer got more than once")
	})
}
