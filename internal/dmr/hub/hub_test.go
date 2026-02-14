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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// makeTestHub creates a Hub with an in-memory SQLite DB, in-memory pubsub, and
// in-memory KV store. It returns the hub and database for seeding test data.
func makeTestHub(t *testing.T) (*hub.Hub, *gorm.DB) {
	t.Helper()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	defConfig.Database.Database = fmt.Sprintf("file:memdb_%p", t)
	defConfig.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}

	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	ps, err := pubsub.MakePubSub(context.Background(), &defConfig)
	require.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

	ct := calltracker.NewCallTracker(database, ps)

	h := hub.NewHub(database, kvStore, ps, ct)

	t.Cleanup(func() {
		h.Stop()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = kvStore.Close()
	})

	return h, database
}

const testSrcID = uint(1000001)
const testRepeaterID = uint(100001)

// makeVoicePacket creates a voice packet suitable for routing.
func makeVoicePacket(dst, streamID uint, groupCall bool, slot bool) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         testSrcID,
		Dst:         dst,
		Repeater:    testRepeaterID,
		GroupCall:   groupCall,
		Slot:        slot,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA,
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
}

// makeVoiceTermPacket creates a voice terminator packet.
func makeVoiceTermPacket(dst, streamID uint, groupCall bool, slot bool) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         testSrcID,
		Dst:         dst,
		Repeater:    testRepeaterID,
		GroupCall:   groupCall,
		Slot:        slot,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceTerm),
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
}

func seedRepeater(t *testing.T, database *gorm.DB, id uint, ownerID uint) {
	t.Helper()
	err := database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{
			Callsign: fmt.Sprintf("T%dST", id),
			ID:       id,
		},
		OwnerID: ownerID,
		Type:    models.RepeaterTypeMMDVM,
	}).Error
	require.NoError(t, err)
}

func seedTalkgroup(t *testing.T, database *gorm.DB, id uint, name string) {
	t.Helper()
	err := database.Create(&models.Talkgroup{
		ID:   id,
		Name: name,
	}).Error
	require.NoError(t, err)
}

func seedUser(t *testing.T, database *gorm.DB, id uint, callsign string) {
	t.Helper()
	err := database.Create(&models.User{
		ID:       id,
		Callsign: callsign,
		Username: callsign,
		Approved: true,
	}).Error
	require.NoError(t, err)
}

// --- Hub lifecycle and server registration tests ---

func TestNewHub(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)
	assert.NotNil(t, h)
}

func TestRegisterServer(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)

	handle := h.RegisterServer(hub.ServerConfig{
		Name: "test-server",
		Role: hub.RoleRepeater,
	})
	defer h.UnregisterServer("test-server")

	assert.NotNil(t, handle)
	assert.Equal(t, "test-server", handle.Name)
	assert.NotNil(t, handle.Packets)
}

func TestRegisterMultipleServers(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)

	h1 := h.RegisterServer(hub.ServerConfig{Name: "srv1", Role: hub.RoleRepeater})
	h2 := h.RegisterServer(hub.ServerConfig{Name: "srv2", Role: hub.RolePeer})
	defer h.UnregisterServer("srv1")
	defer h.UnregisterServer("srv2")

	assert.Equal(t, "srv1", h1.Name)
	assert.Equal(t, "srv2", h2.Name)
}

func TestUnregisterServer(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)

	handle := h.RegisterServer(hub.ServerConfig{Name: "ephemeral", Role: hub.RoleRepeater})
	h.UnregisterServer("ephemeral")

	// Channel should be closed after unregister
	_, ok := <-handle.Packets
	assert.False(t, ok, "expected channel to be closed after UnregisterServer")
}

func TestUnregisterNonexistentServer(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)

	// Should not panic
	assert.NotPanics(t, func() {
		h.UnregisterServer("does-not-exist")
	})
}

func TestStartAndStop(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)

	// Should not panic with no repeaters in DB
	assert.NotPanics(t, func() {
		h.Start()
	})
	assert.NotPanics(t, func() {
		h.Stop()
	})
}

func TestStartWithRepeaters(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 1, "TG1")

	// Add a static talkgroup assignment
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100001).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))

	assert.NotPanics(t, func() {
		h.Start()
	})
}

func TestReloadRepeater(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)

	h.Start()

	// Should not panic
	assert.NotPanics(t, func() {
		h.ReloadRepeater(100001)
	})
}
