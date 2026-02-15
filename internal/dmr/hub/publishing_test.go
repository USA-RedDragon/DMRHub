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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
)

func TestBroadcastServerReceivesGroupCall(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 1, "TG1")

	broadcastHandle := h.RegisterServer(hub.ServerConfig{
		Name:      "broadcast-srv",
		Role:      hub.RoleRepeater,
		Broadcast: true,
	})
	defer h.UnregisterServer("broadcast-srv")

	// Also need a repeater server registered for the source
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)

	// Allow subscription goroutines to fully start
	time.Sleep(100 * time.Millisecond)

	pkt := makeVoicePacket(1, 44444, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-broadcastHandle.Packets:
		assert.Equal(t, uint(1), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for broadcast delivery")
	}
}

func TestBroadcastServerSkipsEcho(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 1, "TG1")

	broadcastHandle := h.RegisterServer(hub.ServerConfig{
		Name:      "echo-srv",
		Role:      hub.RoleRepeater,
		Broadcast: true,
	})
	defer h.UnregisterServer("echo-srv")

	// Route with sourceName = "echo-srv" — should not be delivered back
	pkt := makeVoicePacket(1, 44445, true, false)
	h.RoutePacket(context.Background(), pkt, "echo-srv")

	select {
	case <-broadcastHandle.Packets:
		t.Fatal("Broadcast server should not receive its own echo")
	case <-time.After(500 * time.Millisecond):
		// Expected: no delivery
	}
}

func TestPeerServerDoesNotForwardToSelf(t *testing.T) {
	t.Parallel()
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
}

// --- Benchmarks ---
// These benchmark the end-to-end publish pipeline: Packet.Encode → RawDMRPacket.MarshalMsg → pubsub.Publish
// which is what publishToRepeater and publishForBroadcastServers do internally.

func makeTestPubSubB(b *testing.B) pubsub.PubSub {
	b.Helper()
	defConfig, err := configulator.New[config.Config]().Default()
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

func BenchmarkPublishForBroadcastServers(b *testing.B) {
	ps := makeTestPubSubB(b)
	sub := ps.Subscribe("hub:packets:broadcast")
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
	sourceName := "mmdvm"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encodedPacket := pkt.Encode()
		nameBytes := []byte(sourceName)
		msg := make([]byte, 0, 1+len(nameBytes)+len(encodedPacket))
		msg = append(msg, byte(len(nameBytes)))
		msg = append(msg, nameBytes...)
		msg = append(msg, encodedPacket...)
		_ = ps.Publish("hub:packets:broadcast", msg)
		<-sub.Channel()
	}
}
