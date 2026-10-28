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
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// topicRecorder is a pubsub that records the topics published to it.
type topicRecorder struct {
	pubsub.PubSub
	mu     sync.Mutex
	topics []string
}

func (r *topicRecorder) Publish(topic string, message []byte) error {
	r.mu.Lock()
	r.topics = append(r.topics, topic)
	r.mu.Unlock()
	if err := r.PubSub.Publish(topic, message); err != nil {
		return fmt.Errorf("publishing to %s: %w", topic, err)
	}
	return nil
}

func (r *topicRecorder) packetTopics() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var topics []string
	for _, topic := range r.topics {
		if strings.HasPrefix(topic, "hub:packets:") {
			topics = append(topics, topic)
		}
	}
	return topics
}

func TestGroupCallPublishesOnlyItsTalkgroupTopic(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, database, ps := makeTestHubWithPubSub(t)
		rec := &topicRecorder{PubSub: ps}
		defConfig, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		kvStore, err := kv.MakeKV(context.Background(), &defConfig)
		require.NoError(t, err)
		ct := calltracker.NewCallTracker(database, ps, kvStore)
		h := hub.NewHub(database, kvStore, rec, ct)
		t.Cleanup(func() {
			h.Stop(context.Background())
			ct.Stop()
			_ = kvStore.Close()
		})

		seedUser(t, database, 1000001, "TESTUSER")
		seedRepeater(t, database, 100001, 1000001)
		seedTalkgroup(t, database, 1, "TG1")

		h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		defer h.UnregisterServer(models.RepeaterTypeMMDVM)

		sub := ps.Subscribe("hub:packets:talkgroup:1")
		defer func() { _ = sub.Close() }()

		knowTalkgroups(t, h)
		pkt := makeVoicePacket(1, 44444, true, false)
		h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
		select {
		case <-sub.Channel():
		case <-time.After(failAfter):
			t.Fatal("Timed out waiting for the talkgroup publish")
		}
		settle(t, h, pkt, models.RepeaterTypeMMDVM)

		assert.Equal(t, []string{"hub:packets:talkgroup:1"}, rec.packetTopics())
	})
}

func TestPeerServerDoesNotForwardToSelf(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)

		seedUser(t, database, 1000001, "TESTUSER")
		seedRepeater(t, database, 100001, 1000001)
		seedTalkgroup(t, database, 1, "TG1")

		h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		defer h.UnregisterServer(models.RepeaterTypeMMDVM)

		peerHandle := h.RegisterServer(hub.ServerConfig{Name: "peer-srv", Role: hub.RolePeer})
		defer h.UnregisterServer("peer-srv")

		// When a peer sends a packet, it should NOT be forwarded back to peers
		pkt := makeVoicePacket(1, 55558, true, false)
		h.RoutePacket(context.Background(), pkt, "peer-srv")

		// Peer channel should remain empty (no echo to peers)
		select {
		case <-peerHandle.Packets:
			t.Fatal("Peer server should not receive packets it originates")
		case <-time.After(500 * time.Millisecond):
			// Expected
		}
	})
}

func TestPeerPublishRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)

		seedUser(t, database, 1000001, "TESTUSER")
		seedRepeater(t, database, 100001, 1000001)
		seedTalkgroup(t, database, 1, "TG1")

		// Create a peer with egress enabled and a rule that matches our packet source
		owner := models.User{ID: 2000001, Callsign: "PEEROWNER", Username: "PEEROWNER", Approved: true}
		require.NoError(t, database.Create(&owner).Error)
		peer := models.Peer{ID: 200, OwnerID: owner.ID, Egress: true, Ingress: false}
		require.NoError(t, database.Create(&peer).Error)
		// Egress rule: direction=false (egress), covers source ID 1000001
		rule := models.PeerRule{PeerID: peer.ID, Direction: false, SubjectIDMin: 1000000, SubjectIDMax: 2000000}
		require.NoError(t, database.Create(&rule).Error)
		h.PeerChanged(peer.ID)

		h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		defer h.UnregisterServer(models.RepeaterTypeMMDVM)

		peerHandle := h.RegisterServer(hub.ServerConfig{Name: "peer-srv", Role: hub.RolePeer})
		defer h.UnregisterServer("peer-srv")

		// Cancel the context BEFORE routing — the peer publish goroutine should bail out
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		pkt := makeVoicePacket(1, 77777, true, false)
		h.RoutePacket(ctx, pkt, models.RepeaterTypeMMDVM)

		// The peer channel should remain empty because the context was already cancelled
		select {
		case <-peerHandle.Packets:
			t.Fatal("Peer should not receive packet when context is cancelled")
		case <-time.After(500 * time.Millisecond):
			// Expected: no delivery because context was cancelled
		}
	})
}

func TestPeerEgressKeepsStreamOrder(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)

		seedUser(t, database, 1000001, "TESTUSER")
		seedRepeater(t, database, 100001, 1000001)
		seedTalkgroup(t, database, 1, "TG1")

		owner := models.User{ID: 2000001, Callsign: "PEEROWNER", Username: "PEEROWNER", Approved: true}
		require.NoError(t, database.Create(&owner).Error)
		peer := models.Peer{ID: 200, OwnerID: owner.ID, Egress: true, Ingress: false}
		require.NoError(t, database.Create(&peer).Error)
		rule := models.PeerRule{PeerID: peer.ID, Direction: false, SubjectIDMin: 1000000, SubjectIDMax: 2000000}
		require.NoError(t, database.Create(&rule).Error)
		h.PeerChanged(peer.ID)

		h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		defer h.UnregisterServer(models.RepeaterTypeMMDVM)

		peerHandle := h.RegisterServer(hub.ServerConfig{Name: "peer-srv", Role: hub.RolePeer})
		defer h.UnregisterServer("peer-srv")
		knowTalkgroups(t, h)
		require.NoError(t, h.KnowPeers())

		const count = 90
		for i := range count {
			pkt := makeVoicePacket(1, 88888, true, false)
			pkt.Seq = uint(i)
			h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
		}

		got := make([]uint, 0, count)
		for len(got) < count {
			select {
			case rp := <-peerHandle.Packets:
				got = append(got, rp.Packet.Seq)
			case <-time.After(failAfter):
				t.Fatalf("got %d of %d peer packets", len(got), count)
			}
		}
		want := make([]uint, count)
		for i := range want {
			want[i] = uint(i)
		}
		assert.Equal(t, want, got)
	})
}

// --- Benchmarks ---
// These benchmark the end-to-end publish pipeline: Packet.Encode → RawDMRPacket.MarshalMsg → pubsub.Publish
// which is what publishToRepeater does internally.

func makeTestPubSubB(b *testing.B) pubsub.PubSub {
	b.Helper()
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		b.Fatalf("Failed to create default config: %v", err)
	}
	ps, err := pubsub.MakePubSub(context.Background(), &defConfig)
	if err != nil {
		b.Fatalf("Failed to create pubsub: %v", err)
	}
	b.Cleanup(func() {
		_ = ps.Close()
	})
	return ps
}

func BenchmarkPublishToRepeater(b *testing.B) {
	ps := makeTestPubSubB(b)
	sub := ps.Subscribe(fmt.Sprintf("hub:packets:repeater:%d", 307201))
	defer func() { _ = sub.Close() }()

	pkt := models.Packet{
		Signature:   "DMRD",
		Seq:         1,
		Src:         1000001,
		Dst:         1,
		Repeater:    307201,
		Slot:        false,
		GroupCall:   true,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead),
		StreamID:    42,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var rawPacket models.RawDMRPacket
		rawPacket.Data = pkt.Encode()
		packedBytes, _ := rawPacket.MarshalMsg(nil)
		_ = ps.Publish(fmt.Sprintf("hub:packets:repeater:%d", 307201), packedBytes)
		<-sub.Channel()
	}
}
