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
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const slowRedisCall = 500 * time.Millisecond

// slowRedis makes the KV calls on the keys with prefix and the publishes
// to topics take slowRedisCall each, ignoring their contexts, as a hung
// Redis does.
type slowRedis struct {
	on     atomic.Bool
	prefix string
	topics []string
}

func (s *slowRedis) wait(slow bool) {
	if slow && s.on.Load() {
		time.Sleep(slowRedisCall)
	}
}

type slowRedisKV struct {
	kv.KV
	s *slowRedis
}

func (k slowRedisKV) Has(ctx context.Context, key string) (bool, error) {
	k.s.wait(strings.HasPrefix(key, k.s.prefix))
	return k.KV.Has(ctx, key) //nolint:wrapcheck
}

func (k slowRedisKV) Get(ctx context.Context, key string) ([]byte, error) {
	k.s.wait(strings.HasPrefix(key, k.s.prefix))
	return k.KV.Get(ctx, key) //nolint:wrapcheck
}

func (k slowRedisKV) Set(ctx context.Context, key string, value []byte) error {
	k.s.wait(strings.HasPrefix(key, k.s.prefix))
	return k.KV.Set(ctx, key, value) //nolint:wrapcheck
}

func (k slowRedisKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	k.s.wait(strings.HasPrefix(key, k.s.prefix))
	return k.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

func (k slowRedisKV) Delete(ctx context.Context, key string) error {
	k.s.wait(strings.HasPrefix(key, k.s.prefix))
	return k.KV.Delete(ctx, key) //nolint:wrapcheck
}

type slowRedisPubSub struct {
	pubsub.PubSub
	s *slowRedis
}

func (p slowRedisPubSub) Publish(topic string, message []byte) error {
	p.s.wait(slices.Contains(p.s.topics, topic))
	return p.PubSub.Publish(topic, message) //nolint:wrapcheck
}

func forAllSlowRedisBackends(t *testing.T, prefix string, topics []string, f func(t *testing.T, stack *testutils.IntegrationStack, slow *slowRedis)) {
	t.Helper()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			slow := &slowRedis{prefix: prefix, topics: topics}
			be.WrapKV = func(k kv.KV) kv.KV { return slowRedisKV{KV: k, s: slow} }
			be.WrapPubSub = func(p pubsub.PubSub) pubsub.PubSub { return slowRedisPubSub{PubSub: p, s: slow} }
			f(t, testutils.SetupIntegrationStack(t, be), slow)
		})
	}
}

// TestOpenBridgeSlowRedisHoldsNoBurst sends calls from an OpenBridge peer
// and from a local repeater, and a BCSQ, while Redis takes 500 ms for the
// source quenches and the peer events. Each burst must reach the other
// peer and the repeater in less than half of that, and a BCSQ right
// after another must stop its stream at once.
func TestOpenBridgeSlowRedisHoldsNoBurst(t *testing.T) {
	t.Parallel()
	forAllSlowRedisBackends(t, "dmrhub:openbridge:quench:", []string{"hub:events:peers", "openbridge:egress"}, func(t *testing.T, stack *testutils.IntegrationStack, slow *slowRedis) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		const fromPassword = "from-peer-password"
		stack.SeedPeer(t, 500001, 1000001, fromPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, 500001, true, 1, 1)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		t.Cleanup(mc.Close)
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))

		from := testutils.NewOpenBridgeClient(500001, fromPassword)
		t.Cleanup(from.Close)
		require.NoError(t, from.Connect(stack.OpenBridgeAddr))
		to := testutils.NewOpenBridgeClient(500002, obPassword)
		t.Cleanup(to.Close)
		require.NoError(t, to.Connect(stack.OpenBridgeAddr))
		addr := to.LocalAddr()
		stack.SeedPeer(t, 500002, 1000001, obPassword, addr.IP.String(), addr.Port, false, true)
		stack.SeedPeerRule(t, 500002, false, 1, 9999999)
		time.Sleep(settleDuration)

		send := func(fromPeer bool) func(uint) []time.Time {
			return func(stream uint) []time.Time {
				sent := make([]time.Time, 0, 8)
				start := time.Now()
				for i := range 8 {
					p := makeGroupVoicePacket(1000001, 1, stream, false)
					p.Seq = uint(i)
					due := start.Add(time.Duration(i) * burstPeriod)
					time.Sleep(time.Until(due))
					sent = append(sent, due)
					if fromPeer {
						p.Repeater = 500001
						require.NoError(t, from.SendRawPacket(p))
					} else {
						require.NoError(t, mc.SendDMRD(p))
					}
				}
				return sent
			}
		}

		worstDelays(t, 8101, send(true), to.ReceivedPackets(), mc.ReceivedPackets())
		worstDelays(t, 8102, send(false), to.ReceivedPackets())
		time.Sleep(200 * time.Millisecond)

		slow.on.Store(true)
		defer slow.on.Store(false)
		fromPeer := worstDelays(t, 8103, send(true), to.ReceivedPackets(), mc.ReceivedPackets())
		fromRepeater := worstDelays(t, 8104, send(false), to.ReceivedPackets())
		t.Logf("worst delay with %v per Redis call: peer to peer %v, peer to repeater %v, repeater to peer %v",
			slowRedisCall, fromPeer[0], fromPeer[1], fromRepeater[0])
		assert.Less(t, fromPeer[0], slowRedisCall/2, "a burst from a peer to a peer waited on Redis")
		assert.Less(t, fromPeer[1], slowRedisCall/2, "a burst from a peer to a repeater waited on Redis")
		assert.Less(t, fromRepeater[0], slowRedisCall/2, "a burst from a repeater to a peer waited on Redis")

		require.NoError(t, to.SendControl(dmrd.BridgeControl{Command: dmrd.CommandBCSQ, TalkGroup: 1, Stream: 8105}))
		require.NoError(t, to.SendControl(dmrd.BridgeControl{Command: dmrd.CommandBCSQ, TalkGroup: 1, Stream: 8106}))
		time.Sleep(100 * time.Millisecond)
		require.NoError(t, mc.SendDMRD(makeGroupVoicePacket(1000001, 1, 8106, false)))
		var quenched int
		for _, p := range to.Drain(drainWait) {
			if p.StreamID == 8106 {
				quenched++
			}
		}
		assert.Zero(t, quenched, "a BCSQ behind another waited on Redis")
	})
}

// TestOpenBridgeSlowRedisHoldsNoBurstOnTheOtherReplica sends a call from
// an OpenBridge peer to the replica that does not hold the egress lease,
// while its hand-offs to the holder take 500 ms each. The bursts must
// reach the local repeater in less than half of that.
func TestOpenBridgeSlowRedisHoldsNoBurstOnTheOtherReplica(t *testing.T) {
	t.Parallel()
	forAllSlowRedisBackends(t, "dmrhub:openbridge:quench:", []string{"hub:events:peers", "openbridge:egress"}, func(t *testing.T, stack *testutils.IntegrationStack, slow *slowRedis) {
		r := startReplicaOB(t, stack)
		_, _, otherAddr := r.leader(t)
		stack.SeedPeer(t, 500001, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, 500001, true, 1, 1)
		stack.SeedPeerRule(t, 500002, false, 1, 9999999)
		from := testutils.NewOpenBridgeClient(500001, obPassword)
		t.Cleanup(from.Close)
		require.NoError(t, from.Connect(otherAddr))
		time.Sleep(settleDuration)

		send := func(stream uint) []time.Time {
			sent := make([]time.Time, 0, 8)
			start := time.Now()
			for i := range 8 {
				p := makeGroupVoicePacket(1000001, 1, stream, false)
				p.Seq = uint(i)
				p.Repeater = 500001
				due := start.Add(time.Duration(i) * burstPeriod)
				time.Sleep(time.Until(due))
				sent = append(sent, due)
				require.NoError(t, from.SendRawPacket(p))
			}
			return sent
		}
		worstDelays(t, 8201, send, r.on1.ReceivedPackets(), r.on2.ReceivedPackets())
		time.Sleep(200 * time.Millisecond)

		slow.on.Store(true)
		defer slow.on.Store(false)
		worst := worstDelays(t, 8202, send, r.on1.ReceivedPackets(), r.on2.ReceivedPackets())
		t.Logf("worst delay with %v per hand-off: to the repeater on the first replica %v, on the second %v", slowRedisCall, worst[0], worst[1])
		assert.Less(t, worst[0], slowRedisCall/2, "a burst waited on a hand-off of egress")
		assert.Less(t, worst[1], slowRedisCall/2, "a burst waited on a hand-off of egress")
	})
}
