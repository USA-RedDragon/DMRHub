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

package hub_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const stalledRedisCall = time.Second

type stallMode int

const (
	stallNone stallMode = iota
	stallTimeout
	stallNotSent
)

type heldMessage struct {
	topic   string
	message []byte
}

// stalledPubSub acts as a Redis that stops for a while on the topics that
// stalls picks. In stallTimeout, as a paused Redis, each publish waits a
// second and fails, and Redis runs it once it goes on. In stallNotSent, as
// a stopped Redis, each publish fails at once and never runs.
type stalledPubSub struct {
	pubsub.PubSub
	stalls func(topic string) bool
	mu     sync.Mutex
	mode   stallMode
	held   []heldMessage
	calls  atomic.Int64
}

func (s *stalledPubSub) Publish(topic string, message []byte) error {
	s.mu.Lock()
	if s.mode == stallNone || !s.stalls(topic) {
		defer s.mu.Unlock()
		return s.PubSub.Publish(topic, message) //nolint:wrapcheck
	}
	s.calls.Add(1)
	if s.mode == stallNotSent {
		s.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		return fmt.Errorf("dial tcp: connection refused: %w", pubsub.ErrNotSent)
	}
	s.mu.Unlock()
	time.Sleep(stalledRedisCall)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode == stallNone {
		_ = s.PubSub.Publish(topic, message)
	} else {
		s.held = append(s.held, heldMessage{topic: topic, message: append([]byte(nil), message...)})
	}
	return errors.New("read tcp: i/o timeout")
}

func (s *stalledPubSub) stall(mode stallMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

func (s *stalledPubSub) resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = stallNone
	for _, m := range s.held {
		_ = s.PubSub.Publish(m.topic, m.message)
	}
	s.held = nil
}

func seedStalledRedisHub(t *testing.T, database *gorm.DB) {
	t.Helper()
	seedUser(t, database, testSrcID, "CALLER")
	seedUser(t, database, 1000002, "TARGET")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedRepeater(t, database, 100002, 1000002)
	seedTalkgroup(t, database, 1, "TG1")
	seedTalkgroup(t, database, 2, "TG2")
	var rpt2 models.Repeater
	require.NoError(t, database.First(&rpt2, 100002).Error)
	require.NoError(t, database.Model(&rpt2).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))
	require.NoError(t, database.Model(&rpt2).Association("TS2StaticTalkgroups").Append(&models.Talkgroup{ID: 2}))
	require.NoError(t, database.Create(&models.Peer{ID: 3100100, OwnerID: testSrcID, Egress: true}).Error)
	require.NoError(t, database.Create(&models.PeerRule{PeerID: 3100100, SubjectIDMin: 1, SubjectIDMax: 9999999}).Error)
	require.NoError(t, database.Create(&models.Call{UserID: new(uint(1000002)), RepeaterID: new(uint(100002)), IngressID: 100002, CreatedAt: time.Now()}).Error)
}

func makeStalledRedisHub(t *testing.T, stalls func(topic string) bool) (*hub.Hub, *stalledPubSub, <-chan arrival) {
	t.Helper()
	stalled := &stalledPubSub{stalls: stalls}
	h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub {
		stalled.PubSub = ps
		return stalled
	}, func(k kv.KV) kv.KV { return k })
	seedStalledRedisHub(t, database)
	arrivals := collect(map[string]*hub.ServerHandle{
		"mmdvm": h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater}),
		"peers": h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer}),
	})
	h.ActivateRepeater(context.Background(), testRepeaterID)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)
	worstDelay(t, arrivals, 70200, sendStream(h, 1, 70200, true, 4), "mmdvm:100002", "peers:3100100")
	worstDelay(t, arrivals, 70210, sendSlotStream(h, 2, 70210, true, 4), "mmdvm:100002", "peers:3100100")
	return h, stalled, arrivals
}

func sendSlotStream(h *hub.Hub, dst, stream uint, ts2 bool, n int) []time.Time {
	sent := make([]time.Time, n)
	for i := range n {
		pkt := makeVoicePacket(dst, stream, true, ts2)
		pkt.Seq = uint(i)
		sent[i] = time.Now()
		h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
		time.Sleep(burstPeriod)
	}
	return sent
}

// deliveries is what each target got of each stream: the sequence numbers
// in the order they came, and the worst delay after they were sent.
type deliveries struct {
	seqs  map[string][]uint
	worst map[string]time.Duration
}

func gather(arrivals <-chan arrival, sent map[uint][]time.Time, want map[string]int, quiet time.Duration) deliveries {
	d := deliveries{seqs: map[string][]uint{}, worst: map[string]time.Duration{}}
	done := func() bool {
		for k, n := range want {
			if len(d.seqs[k]) < n {
				return false
			}
		}
		return true
	}
	deadline := time.After(30 * time.Second)
	for {
		wait := quiet
		if !done() {
			wait = 30 * time.Second
		}
		select {
		case a := <-arrivals:
			times, ok := sent[a.rp.Packet.StreamID]
			if !ok {
				continue
			}
			k := fmt.Sprintf("%d %s:%d", a.rp.Packet.StreamID, a.server, a.rp.RepeaterID)
			d.seqs[k] = append(d.seqs[k], a.rp.Packet.Seq)
			d.worst[k] = max(d.worst[k], a.at.Sub(times[a.rp.Packet.Seq]))
		case <-time.After(wait):
			return d
		case <-deadline:
			return d
		}
	}
}

func assertAllInOrder(t *testing.T, d deliveries, key string, n int) {
	t.Helper()
	seqs := d.seqs[key]
	assert.Len(t, seqs, n, "%s: bursts missing or twice", key)
	assert.IsIncreasing(t, seqs, "%s: the bursts came out of order", key)
}

// TestAStalledPubSubHoldsNoBurst sends a group call while every publish to
// its talkgroup waits a second and fails, as a paused Redis makes it, for
// three seconds, and another call on the other slot meanwhile. The routing
// worker of the stalled slot must not wait on the publishes: its bursts to
// the peer, which do not go through pubsub, come at once, as do the bursts
// of the other slot. Once Redis goes on, every burst must reach the
// repeater once and in order.
func TestAStalledPubSubHoldsNoBurst(t *testing.T) {
	t.Parallel()
	h, stalled, arrivals := makeStalledRedisHub(t, func(topic string) bool { return topic == "hub:packets:talkgroup:1" })

	const stream, other, bursts = 70201, 70211, 120
	stalled.stall(stallTimeout)
	time.AfterFunc(3*time.Second, stalled.resume)
	sentOther := make(chan []time.Time, 1)
	go func() { sentOther <- sendSlotStream(h, 2, other, true, bursts) }()
	sent := map[uint][]time.Time{stream: sendStream(h, 1, stream, true, bursts)}
	sent[other] = <-sentOther

	d := gather(arrivals, sent, map[string]int{
		"70201 mmdvm:100002": bursts, "70201 peers:3100100": bursts,
		"70211 mmdvm:100002": bursts, "70211 peers:3100100": bursts,
	}, time.Second)
	t.Logf("worst delays %v after %d stalled publishes", d.worst, stalled.calls.Load())
	assert.Less(t, d.worst["70201 peers:3100100"], stalledRedisCall/4, "a burst to the peer waited on a stalled publish")
	assert.Less(t, d.worst["70211 mmdvm:100002"], stalledRedisCall/4, "a burst of the other slot waited on a stalled publish")
	assert.Less(t, d.worst["70211 peers:3100100"], stalledRedisCall/4, "a burst of the other slot to the peer waited on a stalled publish")
	for _, k := range []string{"70201 mmdvm:100002", "70201 peers:3100100", "70211 mmdvm:100002", "70211 peers:3100100"} {
		assertAllInOrder(t, d, k, bursts)
	}
}

// TestAPublishThatDidNotReachRedisIsSentAgain sends a group call while every
// publish fails at once without reaching Redis, as when Redis is down, for
// two seconds. No burst may be lost: each must reach the repeater once,
// in order, after Redis is back.
func TestAPublishThatDidNotReachRedisIsSentAgain(t *testing.T) {
	t.Parallel()
	h, stalled, arrivals := makeStalledRedisHub(t, func(topic string) bool { return strings.HasPrefix(topic, "hub:packets:") })

	const stream, bursts = 70202, 60
	stalled.stall(stallNotSent)
	time.AfterFunc(2*time.Second, stalled.resume)
	sent := map[uint][]time.Time{stream: sendStream(h, 1, stream, true, bursts)}

	d := gather(arrivals, sent, map[string]int{"70202 mmdvm:100002": bursts, "70202 peers:3100100": bursts}, time.Second)
	t.Logf("the repeater got %d of %d bursts after %d failed publishes, worst delays %v", len(d.seqs["70202 mmdvm:100002"]), bursts, stalled.calls.Load(), d.worst)
	assertAllInOrder(t, d, "70202 mmdvm:100002", bursts)
	assertAllInOrder(t, d, "70202 peers:3100100", bursts)
	assert.Less(t, d.worst["70202 peers:3100100"], stalledRedisCall/4, "a burst to the peer waited on a failed publish")
}

type stalledPlugins struct {
	on        atomic.Bool
	delivered atomic.Int32
}

func (s *stalledPlugins) Owner(id uint) (uint, bool) { return 1, id == 1 }

func (s *stalledPlugins) Deliver(uint, models.Packet) error {
	if s.on.Load() {
		time.Sleep(stalledRedisCall)
	}
	s.delivered.Add(1)
	return nil
}

// TestAStalledPluginDeliveryHoldsNoBurst sends a group call to a talkgroup
// that a plugin owns while each hand-off to the plugin, a publish, waits a
// second. The bursts to the peer must not wait on the plugin, and the
// plugin must get every burst.
func TestAStalledPluginDeliveryHoldsNoBurst(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedStalledRedisHub(t, database)
	arrivals := collect(map[string]*hub.ServerHandle{
		"mmdvm": h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater}),
		"peers": h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer}),
	})
	h.ActivateRepeater(context.Background(), testRepeaterID)
	h.ActivateRepeater(context.Background(), 100002)
	plugin := &stalledPlugins{}
	h.SetPlugins(plugin)
	time.Sleep(100 * time.Millisecond)
	worstDelay(t, arrivals, 70400, sendStream(h, 1, 70400, true, 4), "mmdvm:100002", "peers:3100100")

	plugin.on.Store(true)
	worst := worstDelay(t, arrivals, 70401, sendStream(h, 1, 70401, true, 16), "peers:3100100")
	plugin.on.Store(false)
	t.Logf("worst delay to the peer %v with %v per hand-off to the plugin", worst, stalledRedisCall)
	assert.Less(t, worst, stalledRedisCall/4, "a burst to the peer waited on the plugin")
	assert.Eventually(t, func() bool { return plugin.delivered.Load() == 20 }, 30*time.Second, 50*time.Millisecond, "the plugin did not get every burst")
}

// blockedPubSub holds every publish to a burst topic until release, as a
// Redis that is down for good with no timeout would.
type blockedPubSub struct {
	pubsub.PubSub
	release chan struct{}
	took    atomic.Int64
}

func (b *blockedPubSub) Publish(topic string, message []byte) error {
	if strings.HasPrefix(topic, "hub:packets:") {
		<-b.release
		b.took.Add(1)
	}
	return b.PubSub.Publish(topic, message) //nolint:wrapcheck
}

// TestTheOutboxHoldsALongOutage routes 20000 bursts, about 20 minutes of
// one slot, while pubsub takes none. The outbox must hold them all, and
// publish them all once pubsub is back. It reports the memory that each
// burst in the outbox takes, so it does not run in parallel with other
// tests.
func TestTheOutboxHoldsALongOutage(t *testing.T) { //nolint:paralleltest
	blocked := &blockedPubSub{release: make(chan struct{})}
	h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub {
		blocked.PubSub = ps
		return blocked
	}, func(k kv.KV) kv.KV { return k })
	seedStalledRedisHub(t, database)
	h.ActivateRepeater(context.Background(), testRepeaterID)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	const bursts, batch = 20000, 250
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range bursts {
		p := makeVoicePacket(1, 70500, true, false)
		p.Seq = uint(i % 256)
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		if i%batch == batch-1 {
			require.Eventually(t, func() bool { return h.OutboxDepth() == int64(i+1) }, 10*time.Second, time.Millisecond, "the outbox did not take burst %d", i)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	perBurst := float64(after.HeapAlloc-before.HeapAlloc) / bursts
	t.Logf("%d bursts in the outbox take %.1f MB of heap, %.0f bytes each", bursts, float64(after.HeapAlloc-before.HeapAlloc)/1e6, perBurst)
	assert.Less(t, perBurst, 600.0, "a burst in the outbox takes too much memory")

	close(blocked.release)
	require.Eventually(t, func() bool { return h.OutboxDepth() == 0 }, 30*time.Second, 10*time.Millisecond, "the outbox did not empty")
	assert.Equal(t, int64(bursts), blocked.took.Load(), "pubsub did not get every burst once")
}
