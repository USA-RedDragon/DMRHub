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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoutePacketGroupVoice(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 1, "TG1")

	// Add static talkgroup to second repeater
	var rpt2 models.Repeater
	require.NoError(t, database.First(&rpt2, 100002).Error)
	require.NoError(t, database.Model(&rpt2).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Allow subscription goroutines to fully start
	time.Sleep(100 * time.Millisecond)

	pkt := makeVoicePacket(1, 55555, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	// The packet should be published and delivered to the talkgroup subscriber
	// Wait for delivery — it goes through pubsub async
	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(1), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for routed packet")
	}
}

func TestRoutePacketPrivateCallToRepeater(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedRepeater(t, database, 100002, 1000001)

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Allow subscription goroutines to fully start
	time.Sleep(100 * time.Millisecond)

	// Private call to repeater 100002
	pkt := makeVoicePacket(100002, 55556, false, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(100002), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for private call to repeater")
	}
}

func TestRoutePacketPrivateCallToUser(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "CALLER")
	seedUser(t, database, 1000002, "TARGET")
	seedRepeater(t, database, 100001, 1000001)
	seedRepeater(t, database, 100002, 1000002)

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Allow subscription goroutines to fully start
	time.Sleep(100 * time.Millisecond)

	// Private call to user 1000002 (7-digit = user ID)
	pkt := makeVoicePacket(1000002, 55557, false, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	// Should be delivered to user's owned repeater 100002
	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for private call to user")
	}
}

func TestRoutePacketNonexistentTalkgroup(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)

	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Group call to non-existent talkgroup
	pkt := makeVoicePacket(9999, 11111, true, false)
	assert.NotPanics(t, func() {
		h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	})
}

func TestRoutePacketNonexistentRepeaterDst(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)

	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Private call to non-existent repeater
	pkt := makeVoicePacket(999999, 22222, false, false)
	assert.NotPanics(t, func() {
		h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	})
}

func TestRoutePacketNonexistentUserDst(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)

	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Private call to non-existent user (7-digit)
	pkt := makeVoicePacket(9999999, 33333, false, false)
	assert.NotPanics(t, func() {
		h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	})
}

func TestRoutePacketDataPacket(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)

	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Data packet (FrameDataSync with non-voice dtype)
	dataPkt := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         1000001,
		Dst:         100002,
		Repeater:    100001,
		GroupCall:   false,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: 5,
		StreamID:    66666,
		BER:         -1,
		RSSI:        -1,
	}
	// Should not panic
	assert.NotPanics(t, func() {
		h.RoutePacket(context.Background(), dataPkt, models.RepeaterTypeMMDVM)
	})
}

func TestMultipleRepeatersReceiveGroupCall(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source repeater — no static TGs
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 10, "TG10")

	// Only the destination repeater subscribes to TG10.
	// The source repeater (100001) has no static TG for TG10, so it won't spawn
	// a subscribeTG goroutine for this talkgroup, avoiding the in-memory pubsub
	// single-channel race condition.
	var rpt2 models.Repeater
	require.NoError(t, database.First(&rpt2, 100002).Error)
	require.NoError(t, database.Model(&rpt2).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 10}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Allow subscription goroutines to fully start
	time.Sleep(250 * time.Millisecond)

	// Source repeater 100001 sends to TG10
	pkt := makeVoicePacket(10, 55559, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	// Repeater 100002 should receive it
	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(10), rp.Packet.Dst)
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for group call delivery to repeater 100002")
	}
}

func TestVoiceTerminatorEndsCall(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 1, "TG1")

	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()

	// Voice header
	headerPkt := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         1000001,
		Dst:         1,
		Repeater:    100001,
		GroupCall:   true,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead),
		StreamID:    12345,
		BER:         -1,
		RSSI:        -1,
	}
	h.RoutePacket(context.Background(), headerPkt, models.RepeaterTypeMMDVM)

	// Voice terminator
	termPkt := makeVoiceTermPacket(1, 12345, true, false)
	assert.NotPanics(t, func() {
		h.RoutePacket(context.Background(), termPkt, models.RepeaterTypeMMDVM)
	})
}
