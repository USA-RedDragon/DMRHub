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

package openbridge

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const orderPeer = 500002

type orderRig struct {
	ps   pubsub.PubSub
	peer *net.UDPConn
}

func newOrderRig(t *testing.T) *orderRig {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	return &orderRig{ps: ps, peer: peer}
}

func (r *orderRig) replica(t *testing.T, id string, c clock.Clock) (*Server, pubsub.Subscription) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	at, ok := r.peer.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	peer := &knownPeer{Peer: models.Peer{ID: orderPeer, Password: "password", IP: at.IP.String(), Port: at.Port, Egress: true}}
	s := &Server{
		clock: c, pubsub: r.ps, id: id, Server: conn,
		done: make(chan struct{}), egressOut: make(chan outMessage, outboxSize),
		markers: xsync.NewMap[string, chan struct{}](), quenched: xsync.NewMap[quench, time.Time](),
		released: make(chan struct{}, 1), handedOver: make(chan struct{}),
		peers: lookup.New(clock.Real(), "test peers", lookup.TTL, func(struct{}) (peerTable, error) {
			return peerTable{byID: map[uint]*knownPeer{orderPeer: peer}, all: []*knownPeer{peer}}, nil
		}),
	}
	sub := r.ps.Subscribe(egressTopic)
	t.Cleanup(func() { _ = sub.Close() })
	return s, sub
}

func (r *orderRig) publish(t *testing.T, j any) {
	t.Helper()
	b, err := json.Marshal(j)
	require.NoError(t, err)
	require.NoError(t, r.ps.Publish(egressTopic, b))
}

func (r *orderRig) seqs(t *testing.T, n int) []uint {
	t.Helper()
	require.NoError(t, r.peer.SetReadDeadline(time.Now().Add(handoverWait)))
	out := make([]uint, 0, n)
	buf := make([]byte, 512)
	for range n {
		m, _, err := r.peer.ReadFromUDP(buf)
		require.NoError(t, err, "the peer got %v and then nothing", out)
		require.Greater(t, m, 4)
		out = append(out, uint(buf[4]))
	}
	return out
}

func (r *orderRig) nothing(t *testing.T) bool {
	t.Helper()
	require.NoError(t, r.peer.SetReadDeadline(time.Now()))
	_, _, err := r.peer.ReadFromUDP(make([]byte, 512))
	return err != nil
}

func feed(s *Server, sub pubsub.Subscription) {
	for {
		select {
		case m := <-sub.Channel():
			s.egressMessage(m)
		default:
			return
		}
	}
}

func flushOut(t *testing.T, s *Server) {
	t.Helper()
	for {
		select {
		case m := <-s.egressOut:
			require.NoError(t, s.pubsub.Publish(m.topic, m.msg))
		default:
			return
		}
	}
}

func orderPacket(seq uint) models.Packet {
	return models.Packet{Signature: "DMRD", Seq: seq, Src: 1, Dst: 1, GroupCall: true, StreamID: 9}
}

func packetJob(seq uint) egressJob {
	p := orderPacket(seq)
	return egressJob{Peer: orderPeer, Packet: p.Encode()}
}

// TestANewHolderSendsItsOwnBurstsAfterThoseItHandedOff hands a burst of a
// replica to the holder of the egress lease through the outbox, and then
// lets that replica take the lease before the outbox published the burst,
// as when the holder stops mid-call. The next burst of the replica must
// reach the peer after the one in the outbox.
func TestANewHolderSendsItsOwnBurstsAfterThoseItHandedOff(t *testing.T) {
	t.Parallel()
	r := newOrderRig(t)
	c, sub := r.replica(t, "c", clock.Real())
	c.sendPacketToPeer(orderPeer, orderPacket(1), origin{})
	c.role.Store(roleLeader)
	c.sendPacketToPeer(orderPeer, orderPacket(2), origin{})
	flushOut(t, c)
	feed(c, sub)
	flushOut(t, c)
	feed(c, sub)
	assert.Equal(t, []uint{1, 2}, r.seqs(t, 2), "the peer got a burst of the new holder after a later one")
}

// TestANewHolderWaitsForTheOldOneToSendWhatCameBeforeItsTakenMessage gives
// the egress lease of a holder that stops to another replica, and lets the
// old holder read the egress topic late, as a starved one does. The old
// holder sends what came on the topic before the taken message of the new
// one, and the new one what came after it, so the new one must wait until
// the old one sent its part, or a peer gets a burst after a later one.
func TestANewHolderWaitsForTheOldOneToSendWhatCameBeforeItsTakenMessage(t *testing.T) {
	t.Parallel()
	r := newOrderRig(t)
	b, bSub := r.replica(t, "b", clock.Real())
	c, cSub := r.replica(t, "c", clock.Real())
	b.role.Store(roleReleasing)
	c.role.Store(roleTaking)
	r.publish(t, map[string]any{"kind": kindRelease, "replica": "b", "hands": true})
	r.publish(t, packetJob(1))
	r.publish(t, egressJob{Kind: kindTaken, Replica: "c"})
	r.publish(t, packetJob(2))
	feed(c, cSub)
	assert.True(t, r.nothing(t), "the new holder sent before the old one sent its part")
	feed(b, bSub)
	feed(c, cSub)
	assert.Equal(t, []uint{1, 2}, r.seqs(t, 2), "the peer got a burst after a later one at the lease move")
	assert.True(t, c.IsEgressLeader())
}

// TestANewHolderDoesNotWaitForAnOldHolderOfAnOlderDMRHub gives the egress
// lease of a holder of an older DMRHub, which says nothing once it sent
// its part, to another replica, as in a rolling upgrade. The new holder
// must send at once after its taken message.
func TestANewHolderDoesNotWaitForAnOldHolderOfAnOlderDMRHub(t *testing.T) {
	t.Parallel()
	r := newOrderRig(t)
	c, cSub := r.replica(t, "c", clock.Real())
	c.role.Store(roleTaking)
	r.publish(t, egressJob{Kind: kindRelease, Replica: "b"})
	r.publish(t, egressJob{Kind: kindTaken, Replica: "c"})
	r.publish(t, packetJob(2))
	feed(c, cSub)
	assert.True(t, c.IsEgressLeader(), "the new holder waits for an old holder that says nothing")
	assert.Equal(t, []uint{2}, r.seqs(t, 1))
}

// TestANewHolderSendsWhenTheOldOneSaysNothingInTime gives the egress lease
// of a holder that stops to another replica, but the old holder never
// says that it sent its part, as when it is killed in the handover. The
// new holder must send what it held after handoverWait.
func TestANewHolderSendsWhenTheOldOneSaysNothingInTime(t *testing.T) {
	t.Parallel()
	r := newOrderRig(t)
	fake := clock.NewFake(time.Now())
	c, cSub := r.replica(t, "c", fake)
	c.role.Store(roleTaking)
	r.publish(t, map[string]any{"kind": kindRelease, "replica": "b", "hands": true})
	r.publish(t, egressJob{Kind: kindTaken, Replica: "c"})
	r.publish(t, packetJob(2))
	c.wg.Add(1)
	go c.consumeEgress(cSub)
	t.Cleanup(func() {
		close(c.done)
		c.wg.Wait()
	})
	ctx, cancel := context.WithTimeout(context.Background(), handoverWait)
	defer cancel()
	require.NoError(t, fake.BlockUntil(ctx, 1), "the new holder did not wait for the old one")
	fake.Advance(handoverWait)
	assert.Equal(t, []uint{2}, r.seqs(t, 1), "the new holder did not send after handoverWait")
}
