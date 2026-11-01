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
	"bytes"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const egressTopic = "openbridge:egress"

type egressGate struct {
	pubsub.PubSub
	armed   atomic.Bool
	packet  atomic.Bool
	marker  atomic.Bool
	release chan struct{}
}

func (g *egressGate) Publish(topic string, message []byte) error {
	if topic == egressTopic && g.armed.Load() {
		if bytes.Contains(message, []byte(`"kind":"marker"`)) {
			g.marker.Store(true)
		} else if !bytes.Contains(message, []byte(`"kind"`)) {
			g.packet.Store(true)
		}
	}
	return g.PubSub.Publish(topic, message) //nolint:wrapcheck
}

func (g *egressGate) Subscribe(topic string) pubsub.Subscription {
	sub := g.PubSub.Subscribe(topic)
	if topic != egressTopic {
		return sub
	}
	held := &heldSubscription{Subscription: sub, ch: make(chan []byte, 64), stop: make(chan struct{})}
	go func() {
		for msg := range sub.Channel() {
			if g.armed.Load() {
				select {
				case <-g.release:
				case <-held.stop:
					return
				}
			}
			select {
			case held.ch <- msg:
			case <-held.stop:
				return
			}
		}
	}()
	return held
}

// TestAMovedTalkerKeepsItsOrderToOpenBridge moves a talking repeater from
// a replica without the OpenBridge egress lease to the replica that holds
// it. The burst before the move waits in the egress topic of the old
// replica, and the burst after it comes in at the holder, which sends its
// own bursts at once. The peer must get the burst before the move first,
// from an MMDVM repeater and from an IPSC peer.
func TestAMovedTalkerKeepsItsOrderToOpenBridge(t *testing.T) {
	t.Parallel()
	for _, ipsc := range []bool{false, true} {
		name := map[bool]string{false: "mmdvm", true: "ipsc"}[ipsc]
		for _, be := range allBackends() {
			t.Run(name+"/"+be.Name, func(t *testing.T) {
				t.Parallel()
				gate := &egressGate{release: make(chan struct{})}
				wrap := testutils.Backend{Name: "egress-gate", Setup: func(*testing.T, *config.Config) {}, WrapPubSub: func(ps pubsub.PubSub) pubsub.PubSub {
					gate.PubSub = ps
					return gate
				}}
				checkMovedTalkerOrder(t, gate, testutils.SetupIntegrationStack(t, be, wrap), ipsc)
			})
		}
	}
}

type movingTalker struct {
	addr func(r *testutils.Replica) string
	send func(i int)
}

func mmdvmTalker(t *testing.T, stack *testutils.IntegrationStack, front *testutils.UDPFront, stream uint) movingTalker {
	t.Helper()
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	talker, err := testutils.NewSessionPeer(100001, testPassword, "")
	require.NoError(t, err)
	require.NoError(t, talker.Connect(front.Addr()))
	t.Cleanup(talker.Close)
	require.NoError(t, talker.WaitReady(handshakeWait))
	waitRecorded(t, stack, 100001)
	return movingTalker{
		addr: func(r *testutils.Replica) string { return r.MMDVMAddr },
		send: func(i int) {
			p := makeGroupVoicePacket(1000001, 1, stream, false)
			p.Seq = uint(i)
			require.NoError(t, talker.SendDMRD(p))
		},
	}
}

func ipscTalker(t *testing.T, stack *testutils.IntegrationStack, front *testutils.UDPFront, stream uint) movingTalker {
	t.Helper()
	stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
	talker := testutils.NewIPSCClient(200001, ipscAuthKey)
	require.NoError(t, talker.Connect(front.Addr()))
	t.Cleanup(talker.Close)
	require.NoError(t, talker.WaitReady(handshakeWait))
	waitClaimed(t, stack, 200001)
	tx := liveVoice(t, call{group: true, dst: 1, src: 1000001})
	pkts := tx.packets(stream)
	return movingTalker{
		addr: func(r *testutils.Replica) string { return r.IPSCAddr },
		send: func(i int) {
			p := pkts[i]
			h := layer2.Hint{Kind: layer2.KindData, DataType: enums.DataType(p.DTypeOrVSeq), HasDataType: true}
			if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
				pos, _ := enums.VoiceBurstAt(int(p.DTypeOrVSeq))
				h = layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: pos}
			}
			require.NoError(t, talker.SendBurst(p.Src, p.Dst, p.GroupCall, p.Slot, uint32(stream), layer2.RawBurst(p.DMRData), h, false))
		},
	}
}

func checkMovedTalkerOrder(t *testing.T, gate *egressGate, stack *testutils.IntegrationStack, ipsc bool) {
	t.Helper()
	const stream, before = 9901, 4
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	regX := testutils.Register(t, stack.Hub, stack.KV)
	holder, _ := stack.SpawnRegisteredReplica(t)
	stack.StartServers(t)
	old := stack.Replica()

	peer := testutils.NewOpenBridgeClient(500002, obPassword)
	t.Cleanup(peer.Close)
	require.NoError(t, peer.Connect(stack.OpenBridgeAddr))
	addr := peer.LocalAddr()
	stack.SeedPeer(t, 500002, 1000001, obPassword, addr.IP.String(), addr.Port, true, true)
	stack.SeedPeerRule(t, 500002, false, 1000001, 1000001)
	takeNotice(t, stack.PubSub, holder.Hub.OnPeerChange, "hub:peers:changed")
	waitFor(t, "the second replica holds the egress lease", holder.OpenBridgeServer.IsEgressLeader)
	require.False(t, stack.OpenBridgeServer.IsEgressLeader(), "the first replica holds the egress lease too")

	front := testutils.NewUDPFront(t)
	var talker movingTalker
	if ipsc {
		front.Add(t, stack.IPSCAddr)
		talker = ipscTalker(t, stack, front, stream)
	} else {
		front.Add(t, stack.MMDVMAddr)
		talker = mmdvmTalker(t, stack, front, stream)
	}
	front.Add(t, talker.addr(holder))

	var mu sync.Mutex
	var seqs []uint8
	got := func() []uint8 {
		mu.Lock()
		defer mu.Unlock()
		return append([]uint8(nil), seqs...)
	}
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		peer.ReceiveBridges(2*deliverWait, func(b []testutils.ReceivedBridge) bool {
			mu.Lock()
			defer mu.Unlock()
			seqs = seqs[:0]
			for _, d := range b {
				seqs = append(seqs, d.Data.Seq)
			}
			return len(seqs) >= before+2
		})
	}()
	for i := range before {
		talker.send(i)
		waitFor(t, "the peer gets a burst from the first replica", func() bool { return len(got()) > i })
	}

	gate.armed.Store(true)
	talker.send(before)
	waitFor(t, "the first replica hands the burst before the move to the holder", gate.packet.Load)
	regX.MarkDraining(t.Context())
	front.Drain(talker.addr(old))
	talker.send(before + 1)
	waitFor(t, "the peer gets the burst after the move, or the first replica marks the end of its bursts", func() bool {
		return len(got()) > before || gate.marker.Load()
	})
	close(gate.release)
	<-collected

	order := got()
	require.Len(t, order, before+2, "the peer did not get every burst")
	assert.True(t, slices.IsSorted(order), "the peer got the bursts of the moved talker out of order: %v", order)
}
