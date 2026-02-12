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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
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

	h.Start()

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

	h.Start()

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

	h.Start()

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
