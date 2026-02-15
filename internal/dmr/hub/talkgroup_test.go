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

func TestRoutePacketUnlink(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 1, "TG1")

	// Set a dynamic talkgroup on TS1
	tgID := uint(1)
	require.NoError(t, database.Model(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100001},
	}).Updates(map[string]interface{}{
		"TS1DynamicTalkgroupID": tgID,
	}).Error)

	h.ActivateRepeater(context.Background(), 100001)

	// Send unlink on TS1 (Slot = false)
	unlinkPkt := makeVoicePacket(4000, 88888, true, false)
	h.RoutePacket(context.Background(), unlinkPkt, models.RepeaterTypeMMDVM)

	// Verify the dynamic talkgroup was cleared
	rpt, err := models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	assert.Nil(t, rpt.TS1DynamicTalkgroupID, "expected TS1 dynamic talkgroup to be unlinked")
}

func TestRoutePacketDynamicTalkgroupLinking(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 2, "TG2")

	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)

	// Group voice call to TG 2 on TS1 (Slot=false) should dynamically link it
	pkt := makeVoicePacket(2, 99999, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	// small delay for DB write
	time.Sleep(200 * time.Millisecond)

	rpt, err := models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	require.NotNil(t, rpt.TS1DynamicTalkgroupID, "expected TS1 dynamic talkgroup to be linked")
	assert.Equal(t, uint(2), *rpt.TS1DynamicTalkgroupID)
}

func TestRoutePacketDynamicTalkgroupLinkingTS2(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 3, "TG3")

	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)

	// Group voice call to TG 3 on TS2 (Slot=true)
	pkt := makeVoicePacket(3, 99998, true, true)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	time.Sleep(200 * time.Millisecond)

	rpt, err := models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	require.NotNil(t, rpt.TS2DynamicTalkgroupID, "expected TS2 dynamic talkgroup to be linked")
	assert.Equal(t, uint(3), *rpt.TS2DynamicTalkgroupID)
}

func TestRoutePacketUnlinkTS2(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 5, "TG5")

	// Set a dynamic talkgroup on TS2
	tgID := uint(5)
	require.NoError(t, database.Model(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100001},
	}).Updates(map[string]interface{}{
		"TS2DynamicTalkgroupID": tgID,
	}).Error)

	h.ActivateRepeater(context.Background(), 100001)

	// Send unlink on TS2 (Slot = true)
	unlinkPkt := makeVoicePacket(4000, 88887, true, true)
	h.RoutePacket(context.Background(), unlinkPkt, models.RepeaterTypeMMDVM)

	rpt, err := models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	assert.Nil(t, rpt.TS2DynamicTalkgroupID, "expected TS2 dynamic talkgroup to be unlinked")
}
