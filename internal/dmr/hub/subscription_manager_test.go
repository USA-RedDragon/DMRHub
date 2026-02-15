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

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
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

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
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

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
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

	h.ActivateRepeater(context.Background(), 100001)
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

	h.ActivateRepeater(context.Background(), 100001)
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

	h.ActivateRepeater(context.Background(), 100001)
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

	h.ActivateRepeater(context.Background(), 100001)
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

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	// Create TG40 and assign it to repeater 100002 after Start
	seedTalkgroup(t, database, 40, "TG40")
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 40}))

	// Reload so the subscription manager picks up the new static TG
	h.ReloadRepeater(context.Background(), 100002)
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

	assert.NotPanics(t, func() {
		h.ReloadRepeater(context.Background(), 999999)
	})
}

// TestActivateRepeaterIdempotent verifies that calling ActivateRepeater
// multiple times does not create duplicate subscriptions — only one packet
// should be delivered per publish.
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

	// ActivateRepeater twice — should not panic or duplicate subscriptions
	assert.NotPanics(t, func() {
		h.ActivateRepeater(context.Background(), 100001)
		h.ActivateRepeater(context.Background(), 100002)
		h.ActivateRepeater(context.Background(), 100001)
		h.ActivateRepeater(context.Background(), 100002)
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

// TestDeactivateRepeaterStopsDelivery verifies that deactivating a repeater
// cancels its subscriptions so packets are no longer delivered.
func TestDeactivateRepeaterStopsDelivery(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source
	seedRepeater(t, database, 100002, 1000001) // destination
	seedTalkgroup(t, database, 70, "TG70")

	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 70}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	// Verify delivery works before deactivation
	pkt := makeVoicePacket(70, 70050, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for delivery before deactivation")
	}

	// Deactivate the destination repeater
	h.DeactivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	// Packets should no longer be delivered
	pkt2 := makeVoicePacket(70, 70051, true, false)
	h.RoutePacket(context.Background(), pkt2, models.RepeaterTypeMMDVM)

	select {
	case <-srvHandle.Packets:
		t.Fatal("Deactivated repeater should not receive packets")
	case <-time.After(500 * time.Millisecond):
		// Expected: no delivery
	}
}

// TestDeleteTalkgroupAndReloadClearsSubscription verifies that after deleting
// a talkgroup and reloading affected repeaters, those repeaters no longer
// receive packets for the deleted talkgroup.
func TestDeleteTalkgroupAndReloadClearsSubscription(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source
	seedRepeater(t, database, 100002, 1000001) // destination
	seedTalkgroup(t, database, 80, "TG80")

	// Assign TG80 as a static talkgroup on repeater 100002
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 80}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	// Verify delivery works before deletion
	pkt := makeVoicePacket(80, 70060, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(80), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for delivery before talkgroup deletion")
	}

	// Delete the talkgroup — this returns affected repeater IDs
	affectedIDs, err := models.DeleteTalkgroup(database, 80)
	require.NoError(t, err)
	assert.Contains(t, affectedIDs, uint(100002), "repeater 100002 should be in the affected list")

	// Reload affected repeaters (simulating what the API handler does)
	for _, rid := range affectedIDs {
		h.ReloadRepeater(context.Background(), rid)
	}
	time.Sleep(100 * time.Millisecond)

	// Packets to the deleted talkgroup should no longer be delivered
	pkt2 := makeVoicePacket(80, 70061, true, false)
	h.RoutePacket(context.Background(), pkt2, models.RepeaterTypeMMDVM)

	select {
	case <-srvHandle.Packets:
		t.Fatal("Repeater should not receive packets for a deleted talkgroup")
	case <-time.After(500 * time.Millisecond):
		// Expected: no delivery after talkgroup deletion
	}
}

// TestDeleteTalkgroupWithDynamicTGReturnsAffectedRepeaters verifies that
// deleting a talkgroup that is dynamically linked returns the affected repeater ID.
func TestDeleteTalkgroupWithDynamicTGReturnsAffectedRepeaters(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source
	seedRepeater(t, database, 100002, 1000001) // destination
	seedTalkgroup(t, database, 90, "TG90")

	// Dynamically link TG90 to TS2 on repeater 100002
	tgID := uint(90)
	require.NoError(t, database.Model(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002},
	}).Updates(map[string]interface{}{
		"TS2DynamicTalkgroupID": tgID,
	}).Error)

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	// Verify delivery works
	pkt := makeVoicePacket(90, 70070, true, true)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for dynamic TG delivery")
	}

	// Delete the talkgroup
	affectedIDs, err := models.DeleteTalkgroup(database, 90)
	require.NoError(t, err)
	assert.Contains(t, affectedIDs, uint(100002))

	// Reload and verify no more delivery
	for _, rid := range affectedIDs {
		h.ReloadRepeater(context.Background(), rid)
	}
	time.Sleep(100 * time.Millisecond)

	pkt2 := makeVoicePacket(90, 70071, true, true)
	h.RoutePacket(context.Background(), pkt2, models.RepeaterTypeMMDVM)

	select {
	case <-srvHandle.Packets:
		t.Fatal("Repeater should not receive packets for deleted dynamic talkgroup")
	case <-time.After(500 * time.Millisecond):
		// Expected
	}
}

// TestReloadRepeaterNoOpForNonConnected verifies that calling ReloadRepeater
// for a repeater that was never activated (i.e. not connected) does NOT create
// any subscriptions. This prevents leaking goroutines when admins edit talkgroup
// assignments for offline repeaters via the API.
func TestReloadRepeaterNoOpForNonConnected(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source (will be activated)
	seedRepeater(t, database, 100002, 1000001) // target (will NOT be activated)
	seedTalkgroup(t, database, 95, "TG95")

	// Assign TG95 as a static talkgroup on the non-connected repeater
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 95}))

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	// Only activate the source — leave 100002 disconnected
	h.ActivateRepeater(context.Background(), 100001)
	time.Sleep(100 * time.Millisecond)

	// Reload the non-connected repeater (simulates admin API call)
	h.ReloadRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	// Send a packet on TG95 — should NOT be delivered to non-connected 100002
	pkt := makeVoicePacket(95, 70080, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		t.Fatalf("Non-connected repeater should not receive packets, got delivery to repeaterID=%d", rp.RepeaterID)
	case <-time.After(500 * time.Millisecond):
		// Expected: no delivery for non-connected repeater
	}
}

// TestSubscribeTGWantRXFalseCleansUpSubscription verifies that when subscribeTG
// receives a packet the repeater no longer wants (WantRX returns false), the
// subscription map entry and context are properly cleaned up. This is a
// regression test: previously the context was leaked and the stale map entry
// prevented re-subscription when the talkgroup was reassigned.
func TestSubscribeTGWantRXFalseCleansUpSubscription(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001) // source
	seedRepeater(t, database, 100002, 1000001) // destination
	seedTalkgroup(t, database, 50, "TG50")

	// Set TG50 as dynamic talkgroup on TS1 for repeater 100002
	tgID := uint(50)
	require.NoError(t, database.Model(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002},
	}).Updates(map[string]interface{}{
		"TS1DynamicTalkgroupID": tgID,
	}).Error)

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	// Verify delivery works initially
	pkt := makeVoicePacket(50, 80001, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(50), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for initial TG50 delivery")
	}

	// Remove the dynamic talkgroup from the DB so WantRX will return false
	require.NoError(t, database.Model(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002},
	}).Updates(map[string]interface{}{
		"TS1DynamicTalkgroupID": nil,
	}).Error)

	// Send another packet — subscribeTG will call WantRX, get false, and exit.
	// Before the fix this leaked the context and left a stale map entry.
	pkt2 := makeVoicePacket(50, 80002, true, false)
	h.RoutePacket(context.Background(), pkt2, models.RepeaterTypeMMDVM)

	// Drain any delivery (shouldn't arrive, but don't let it block)
	select {
	case <-srvHandle.Packets:
	case <-time.After(500 * time.Millisecond):
	}

	// Re-assign the dynamic talkgroup
	require.NoError(t, database.Model(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002},
	}).Updates(map[string]interface{}{
		"TS1DynamicTalkgroupID": tgID,
	}).Error)

	// Re-activate the repeater. If the stale map entry was not cleaned up,
	// activateRepeaterLocked will see it and skip creating a new subscription,
	// meaning this packet will never be delivered.
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	pkt3 := makeVoicePacket(50, 80003, true, false)
	h.RoutePacket(context.Background(), pkt3, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(50), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for TG50 delivery after re-subscription — stale map entry was not cleaned up")
	}
}

// TestStopAllRepeatersDeactivatesEveryRepeater is a regression test for a bug
// where stopAllRepeaters (called by Hub.Stop) modified the subscriptions map
// during Range iteration, which could cause entries to be skipped. After Stop,
// no repeater should receive packets.
func TestStopAllRepeatersDeactivatesEveryRepeater(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedTalkgroup(t, database, 200, "TG200")

	// Create and activate many repeaters to increase the likelihood of
	// exposing skipped entries during map iteration.
	const numRepeaters = 20
	for i := uint(0); i < numRepeaters; i++ {
		id := 200001 + i
		seedRepeater(t, database, id, 1000001)
		var rpt models.Repeater
		require.NoError(t, database.First(&rpt, id).Error)
		require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 200}))
	}

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	for i := uint(0); i < numRepeaters; i++ {
		h.ActivateRepeater(context.Background(), 200001+i)
	}
	time.Sleep(200 * time.Millisecond)

	// Verify at least one repeater receives packets before Stop
	pkt := makeVoicePacket(200, 90000, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	received := false
	for {
		select {
		case <-srvHandle.Packets:
			received = true
			continue
		case <-time.After(500 * time.Millisecond):
		}
		break
	}
	require.True(t, received, "At least one repeater should receive packets before Stop")

	// Stop should cancel ALL subscriptions — none should be skipped.
	h.Stop(context.Background())
	time.Sleep(200 * time.Millisecond)

	// Send another packet — no repeater should receive it.
	pkt2 := makeVoicePacket(200, 90001, true, false)
	h.RoutePacket(context.Background(), pkt2, models.RepeaterTypeMMDVM)

	select {
	case rp := <-srvHandle.Packets:
		t.Fatalf("After Stop, no repeater should receive packets, but repeater %d did", rp.RepeaterID)
	case <-time.After(500 * time.Millisecond):
		// Expected: all subscriptions were canceled
	}
}

// TestDeliverToServerUnblocksOnStop is a regression test for a bug where
// deliverToServer did a bare blocking send on the server channel. If the
// channel was full (slow consumer), the subscription goroutine would block
// indefinitely, stalling all packet delivery on that topic with no way to
// recover even during shutdown. The fix adds a select on the hub's done
// channel so the send is interruptible at shutdown.
func TestDeliverToServerUnblocksOnStop(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 50, "TG50")

	// Assign TG50 to the destination repeater
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 50}))

	// Register a server but NEVER consume from its channel — this simulates
	// a slow/stuck consumer that lets the channel fill up.
	_ = h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	// Flood the channel with more packets than its buffer (500).
	// Each RoutePacket publishes asynchronously via pubsub, so we send many
	// and give them time to be delivered to the server channel.
	for i := uint(0); i < 600; i++ {
		pkt := makeVoicePacket(50, 80000+i, true, false)
		h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	}
	// Give subscription goroutines time to process and fill the channel.
	time.Sleep(500 * time.Millisecond)

	// Stop must complete promptly even though the server channel is full.
	// Before the fix, Stop() would hang because stopAllRepeaters cancels
	// subscription contexts, but the goroutine blocked in deliverToServer
	// never checked its context and couldn't exit.
	done := make(chan struct{})
	go func() {
		h.Stop(context.Background())
		close(done)
	}()

	select {
	case <-done:
		// Stop completed — the blocked deliverToServer send was interrupted.
	case <-time.After(5 * time.Second):
		t.Fatal("Hub.Stop() did not complete within 5s — deliverToServer is likely blocked on a full channel")
	}
}
