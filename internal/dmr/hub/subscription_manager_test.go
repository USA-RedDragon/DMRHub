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
	"github.com/stretchr/testify/require"
)

// TestActivateRepeaterWithDynamicTG verifies that starting the hub with a
// repeater that has a dynamic talkgroup correctly subscribes to that TG.
func TestActivateRepeaterWithDynamicTG(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source — no TGs
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 7, "TG7")

	// Set a dynamic talkgroup on the destination repeater before Start
	tgID := uint(7)
	require.NoError(t, database.Model(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002},
	}).Updates(map[string]interface{}{
		"TS1DynamicTalkgroupID": tgID,
	}).Error)

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()
	time.Sleep(100 * time.Millisecond)

	pkt := makeVoicePacket(7, 70001, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(7), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for dynamic TG delivery")
	}
}

// TestActivateRepeaterBothTimeslotStaticTGs verifies that subscriptions are set
// up for static talkgroups assigned to both TS1 and TS2.
func TestActivateRepeaterBothTimeslotStaticTGs(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 20, "TG20")
	seedTalkgroup(t, database, 21, "TG21")

	// Assign TG20 to TS1 and TG21 to TS2 on repeater 100002
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 20}))
	require.NoError(t, database.Model(&rpt).Association("TS2StaticTalkgroups").Append(&models.Talkgroup{ID: 21}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()
	time.Sleep(100 * time.Millisecond)

	// Send group call on TS1 TG20
	pkt := makeVoicePacket(20, 70002, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(20), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for TS1 static TG delivery")
	}

	// Send group call on TS2 TG21
	pkt2 := makeVoicePacket(21, 70003, true, true)
	h.RoutePacket(context.Background(), pkt2, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(21), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for TS2 static TG delivery")
	}
}

// TestDynamicTGOnBothTimeslots verifies that dynamic talkgroups on both TS1 and
// TS2 are subscribed to simultaneously during activation.
func TestDynamicTGOnBothTimeslots(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 25, "TG25")
	seedTalkgroup(t, database, 26, "TG26")

	// Set dynamic talkgroups on both timeslots
	ts1TGID := uint(25)
	ts2TGID := uint(26)
	require.NoError(t, database.Model(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002},
	}).Updates(map[string]interface{}{
		"TS1DynamicTalkgroupID": ts1TGID,
		"TS2DynamicTalkgroupID": ts2TGID,
	}).Error)

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()
	time.Sleep(100 * time.Millisecond)

	// Verify TS1 dynamic TG
	pkt := makeVoicePacket(25, 70010, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(25), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for TS1 dynamic TG delivery")
	}

	// Verify TS2 dynamic TG
	pkt2 := makeVoicePacket(26, 70011, true, true)
	h.RoutePacket(context.Background(), pkt2, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(26), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for TS2 dynamic TG delivery")
	}
}

// TestSubscribeTGIgnoresEchoFromSameRepeater verifies that subscribeTG filters
// out packets that originate from the subscribed repeater itself.
func TestSubscribeTGIgnoresEchoFromSameRepeater(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 30, "TG30")

	// Repeater 100001 subscribes to TG30 via static assignment
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100001).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 30}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()
	time.Sleep(100 * time.Millisecond)

	// Repeater 100001 sends to TG30 — it should NOT receive its own packet back
	pkt := makeVoicePacket(30, 70004, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case <-srvHandle.Packets:
		t.Fatal("subscribeTG should not echo packets back to the originating repeater")
	case <-time.After(500 * time.Millisecond):
		// Expected: no delivery back to self
	}
}

// TestSimplexRepeaterCrossTimeslotEcho verifies that a simplex repeater
// receives its own talkgroup traffic back on the opposite timeslot.
func TestSimplexRepeaterCrossTimeslotEcho(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 31, "TG31")

	// Enable simplex mode and assign TG31 to TS1
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100001).Error)
	require.NoError(t, database.Model(&rpt).Update("simplex_repeater", true).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 31}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()
	time.Sleep(100 * time.Millisecond)

	// Repeater 100001 sends on TS1 (slot=false) to TG31 — simplex should echo back on TS2 (slot=true)
	pkt := makeVoicePacket(31, 70020, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100001), rp.RepeaterID)
		assert.Equal(t, uint(31), rp.Packet.Dst)
		assert.True(t, rp.Packet.Slot, "Simplex echo should deliver on opposite timeslot (TS2)")
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for simplex cross-timeslot echo")
	}
}

// TestSimplexRepeaterTS2ToTS1 verifies that a simplex repeater echoes
// traffic sent on TS2 back on TS1.
func TestSimplexRepeaterTS2ToTS1(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 32, "TG32")

	// Enable simplex mode and assign TG32 to TS2
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100001).Error)
	require.NoError(t, database.Model(&rpt).Update("simplex_repeater", true).Error)
	require.NoError(t, database.Model(&rpt).Association("TS2StaticTalkgroups").Append(&models.Talkgroup{ID: 32}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()
	time.Sleep(100 * time.Millisecond)

	// Repeater 100001 sends on TS2 (slot=true) to TG32 — simplex should echo back on TS1 (slot=false)
	pkt := makeVoicePacket(32, 70021, true, true)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100001), rp.RepeaterID)
		assert.Equal(t, uint(32), rp.Packet.Dst)
		assert.False(t, rp.Packet.Slot, "Simplex echo should deliver on opposite timeslot (TS1)")
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for simplex TS2→TS1 echo")
	}
}

// TestNonSimplexRepeaterDoesNotEcho verifies that a repeater without
// simplex mode enabled does NOT echo its own packets.
func TestNonSimplexRepeaterDoesNotEcho(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 33, "TG33")

	// SimplexRepeater is false (default), assign TG33 to TS1
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100001).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 33}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()
	time.Sleep(100 * time.Millisecond)

	pkt := makeVoicePacket(33, 70022, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case <-srvHandle.Packets:
		t.Fatal("Non-simplex repeater should not echo packets back to itself")
	case <-time.After(500 * time.Millisecond):
		// Expected: no delivery back to self
	}
}

// TestReloadRepeaterPicksUpNewStaticTG verifies that reloading a repeater after
// adding a new static talkgroup subscribes to the new TG.
func TestReloadRepeaterPicksUpNewStaticTG(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source
	seedRepeater(t, database, 100002, 1000001) // destination — initially no TGs

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.Start()
	time.Sleep(100 * time.Millisecond)

	// Create TG40 and assign it to repeater 100002 after Start
	seedTalkgroup(t, database, 40, "TG40")
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 40}))

	// Reload so the subscription manager picks up the new static TG
	h.ReloadRepeater(100002)
	time.Sleep(100 * time.Millisecond)

	pkt := makeVoicePacket(40, 70005, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(40), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for delivery after reload with new static TG")
	}
}

// TestReloadNonexistentRepeater verifies that reloading a repeater that does not
// exist in the database does not panic.
func TestReloadNonexistentRepeater(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)

	h.Start()

	assert.NotPanics(t, func() {
		h.ReloadRepeater(999999)
	})
}

// TestActivateRepeaterIdempotent verifies that calling Start (which activates
// repeaters) does not create duplicate subscriptions — only one packet should
// be delivered per publish.
func TestActivateRepeaterIdempotent(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 60, "TG60")

	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 60}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	// Start twice — should not panic or duplicate subscriptions
	assert.NotPanics(t, func() {
		h.Start()
		h.Start()
	})
	time.Sleep(100 * time.Millisecond)

	// Verify only ONE packet is delivered (no duplicate subscription)
	pkt := makeVoicePacket(60, 70008, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for delivery")
	}

	// No second packet should arrive
	select {
	case <-srvHandle.Packets:
		t.Fatal("Duplicate subscription detected — received extra packet")
	case <-time.After(500 * time.Millisecond):
		// Expected: no duplicate
	}
}
