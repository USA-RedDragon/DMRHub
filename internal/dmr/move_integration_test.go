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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// moveWait is long enough for a SessionPeer, which pings every 200 ms, to
// send several pings after its session moved.
const moveWait = 1500 * time.Millisecond

type moveRig struct {
	stack    *testutils.IntegrationStack
	second   testutils.Replica
	first    *servers.InstanceRegistry
	front    *testutils.UDPFront
	peer     *testutils.SessionPeer
	listener *testutils.MMDVMClient
}

// startMoveRig runs two replicas behind a UDPFront. Repeater 100001 logs
// in through the front to the first replica; repeater 100002 listens to TG
// 1 on the second.
func startMoveRig(t *testing.T, stack *testutils.IntegrationStack) moveRig {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
	stack.AssignTS1StaticTG(t, 100002, 1)
	r := moveRig{stack: stack}
	r.first = testutils.Register(t, stack.Hub, stack.KV)
	second, _ := stack.SpawnRegisteredReplica(t)
	r.second = *second
	stack.StartServers(t)

	r.front = testutils.NewUDPFront(t)
	r.front.Add(t, stack.MMDVMAddr)
	var err error
	r.peer, err = testutils.NewSessionPeer(100001, testPassword, "")
	require.NoError(t, err)
	require.NoError(t, r.peer.Connect(r.front.Addr()))
	t.Cleanup(r.peer.Close)
	require.NoError(t, r.peer.WaitReady(handshakeWait))

	r.listener = testutils.NewMMDVMClient(100002, "", testPassword)
	require.NoError(t, r.listener.Connect(r.second.MMDVMAddr))
	t.Cleanup(r.listener.Close)
	require.NoError(t, r.listener.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	r.front.Add(t, r.second.MMDVMAddr)
	return r
}

// TestMMDVMMovesFromADrainingReplica drains the replica of a repeater as a
// rolling restart does. The load balancer moves the repeater to the other
// replica from a new port, and that replica must take the session over
// without a new login and route its calls.
func TestMMDVMMovesFromADrainingReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startMoveRig(t, stack)
		r.first.MarkDraining(t.Context())
		r.front.Drain(stack.MMDVMAddr)
		time.Sleep(moveWait)

		require.Equal(t, 1, r.front.Moves(), "the front moved the repeater")
		assert.Zero(t, r.peer.Downs(), "the repeater lost its session")
		assert.Equal(t, 1, r.peer.Ups(), "the repeater logged in again")

		require.NoError(t, r.peer.SendDMRD(makeGroupVoicePacket(1000001, 1, 7301, false)))
		require.NoError(t, r.peer.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7301, false)))
		assert.NotEmpty(t, r.listener.Drain(drainWait), "the other replica routes the calls of the repeater")
	})
}

// TestMMDVMNewPortWithoutADrainLogsIn moves a repeater to the other
// replica from a new port while its replica still serves, as a spoofer at
// another port would. The other replica must not take the session over.
func TestMMDVMNewPortWithoutADrainLogsIn(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startMoveRig(t, stack)
		r.front.Drain(stack.MMDVMAddr)
		time.Sleep(moveWait)

		require.Equal(t, 1, r.front.Moves(), "the front moved the repeater")
		assert.Positive(t, r.peer.Downs(), "a replica that serves keeps its sessions")
	})
}

// TestMMDVMSecondAddressAfterAMoveLogsIn sends a ping of a repeater that
// moved from a third address. The replica that took the session over must
// answer it with MSTNAK and keep serving the repeater.
func TestMMDVMSecondAddressAfterAMoveLogsIn(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startMoveRig(t, stack)
		r.first.MarkDraining(t.Context())
		r.front.Drain(stack.MMDVMAddr)
		time.Sleep(moveWait)
		require.Zero(t, r.peer.Downs())

		ping, err := (&dmrd.Control{Command: dmrd.CommandRPTPING, ID: 100001}).AppendBinary(nil, dmrd.DialectMMDVM)
		require.NoError(t, err)
		assert.Equal(t, dmrd.CommandMSTNAK, dmrd.Classify(exchange(t, r.second.MMDVMAddr, ping), dmrd.DialectMMDVM))

		time.Sleep(moveWait)
		assert.Zero(t, r.peer.Downs(), "the repeater keeps its session")
	})
}

// exchange sends p to addr from a new socket and returns the answer.
func exchange(t *testing.T, addr string, p []byte) []byte {
	t.Helper()
	to, err := net.ResolveUDPAddr("udp", addr)
	require.NoError(t, err)
	conn, err := net.DialUDP("udp", nil, to)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.Write(p)
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(drainWait)))
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	return buf[:n]
}

// TestMMDVMMovesAfterTheClaimExpired lets the claim of the replica on a
// repeater expire, as it does 5 minutes after the login, before the
// replica drains. The pings of the repeater must keep the claim alive, so
// that the move still finds the draining owner.
func TestMMDVMMovesAfterTheClaimExpired(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		r := startMoveRig(t, stack)
		require.NoError(t, stack.KV.Delete(t.Context(), "dmrhub:peer_owner:100001"))
		time.Sleep(moveWait)
		r.first.MarkDraining(t.Context())
		r.front.Drain(stack.MMDVMAddr)
		time.Sleep(moveWait)

		require.Equal(t, 1, r.front.Moves(), "the front moved the repeater")
		assert.Zero(t, r.peer.Downs(), "the repeater lost its session")
	})
}

// TestMMDVMOptionsNeedALoginAfterAMove sends RPTO for a repeater whose
// replica drains, first from another address and then from the address
// that the repeater moved to. Neither may change the static talkgroups of
// the repeater, since no password vouches for them.
func TestMMDVMOptionsNeedALoginAfterAMove(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedTalkgroup(t, 10, "TG10")
		r := startMoveRig(t, stack)
		r.first.MarkDraining(t.Context())
		rpto, err := (&dmrd.Control{Command: dmrd.CommandRPTO, ID: 100001, Text: []byte("TS1=;TS2=10;")}).AppendBinary(nil, dmrd.DialectMMDVM)
		require.NoError(t, err)
		assert.Equal(t, dmrd.CommandMSTNAK, dmrd.Classify(exchange(t, r.second.MMDVMAddr, rpto), dmrd.DialectMMDVM),
			"options do not move a session")

		r.front.Drain(stack.MMDVMAddr)
		time.Sleep(moveWait)
		require.Zero(t, r.peer.Downs())
		require.NoError(t, r.peer.SendRaw(rpto))
		time.Sleep(settleDuration)
		ts1, ts2 := staticIDs(t, stack, 100001)
		assert.Empty(t, ts1)
		assert.Empty(t, ts2, "options of a moved session changed the static talkgroups")
	})
}

// TestMMDVMLoginElsewhereEndsTheOldSession logs a repeater in to one
// replica and then to the other from another address. The first replica
// must end its session, so that only the second sends the calls to the
// repeater.
func TestMMDVMLoginElsewhereEndsTheOldSession(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)
		testutils.Register(t, stack.Hub, stack.KV)
		second, _ := stack.SpawnRegisteredReplica(t)
		stack.StartServers(t)

		old := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, old.Connect(stack.MMDVMAddr))
		defer old.Close()
		require.NoError(t, old.WaitReady(handshakeWait))
		time.Sleep(settleDuration)
		fresh := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, fresh.Connect(second.MMDVMAddr))
		defer fresh.Close()
		require.NoError(t, fresh.WaitReady(handshakeWait))
		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, talker.Connect(second.MMDVMAddr))
		defer talker.Close()
		require.NoError(t, talker.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		sendGroupCall(t, talker.SendDMRD, 1, 7401, false)
		assert.NotEmpty(t, fresh.Drain(drainWait), "the new session gets the call")
		assert.Empty(t, old.Drain(settleDuration), "the old session still got the call")
	})
}

// TestIPSCMovesFromADrainingReplica drains the replica of two IPSC peers,
// one with a key and one without, behind a UDPFront. The peers must keep
// their registration and get the calls from the other replica.
func TestIPSCMovesFromADrainingReplica(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.SeedIPSCRepeater(t, 200002, 1000001, "")
		stack.AssignTS1StaticTG(t, 200001, 1)
		stack.AssignTS1StaticTG(t, 200002, 1)
		first := testutils.Register(t, stack.Hub, stack.KV)
		second, _ := stack.SpawnRegisteredReplica(t)
		stack.StartServers(t)

		front := testutils.NewUDPFront(t)
		front.Add(t, stack.IPSCAddr)
		peers := make([]*testutils.IPSCClient, 0, 2)
		for _, p := range []struct {
			id  uint32
			key string
		}{{200001, ipscAuthKey}, {200002, ""}} {
			c := testutils.NewIPSCClient(p.id, p.key)
			require.NoError(t, c.Connect(front.Addr()))
			t.Cleanup(c.Close)
			require.NoError(t, c.WaitReady(handshakeWait))
			peers = append(peers, c)
		}
		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, talker.Connect(second.MMDVMAddr))
		defer talker.Close()
		require.NoError(t, talker.WaitReady(handshakeWait))
		time.Sleep(settleDuration)
		front.Add(t, second.IPSCAddr)

		first.MarkDraining(t.Context())
		front.Drain(stack.IPSCAddr)
		time.Sleep(2500 * time.Millisecond)
		require.Equal(t, 2, front.Moves(), "the front moved both peers")

		sendGroupCall(t, talker.SendDMRD, 1, 7501, false)
		for _, c := range peers {
			assert.Zero(t, c.Downs(), "peer %d lost its registration", c.PeerID)
			assert.NotEmpty(t, c.Drain(drainWait), "peer %d got no call from the other replica", c.PeerID)
		}
	})
}
