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

package testutils

import (
	"context"
	"fmt"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/ipsc"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/mmdvm"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// IntegrationStack is a full DMRHub test environment with real MMDVM and IPSC
// servers on ephemeral UDP ports, backed by in-memory SQLite, pubsub, and KV.
type IntegrationStack struct {
	Config      *config.Config
	DB          *gorm.DB
	Hub         *hub.Hub
	PubSub      pubsub.PubSub
	KV          kv.KV
	CallTracker *calltracker.CallTracker
	MMDVMServer *mmdvm.Server
	IPSCServer  *ipsc.IPSCServer
	MMDVMAddr   string // host:port of the MMDVM server
	IPSCAddr    string // host:port of the IPSC server
}

// SetupIntegrationStack creates and starts a full integration test environment.
// The hub is NOT started yet — call SeedAndStart() after seeding your DB data.
func SetupIntegrationStack(t *testing.T, backends ...Backend) *IntegrationStack {
	t.Helper()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	// In-memory SQLite — use a named shared-cache DB so all connections within
	// the same test share data. Without cache=shared, each connection in the pool
	// gets its own empty in-memory database, causing "no such table" errors.
	defConfig.Database.Database = fmt.Sprintf("file:memdb_%p", t)
	defConfig.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}

	// Apply backend overrides (e.g. Postgres+Redis instead of SQLite+memory).
	for _, be := range backends {
		be.Setup(t, &defConfig)
	}

	// Ephemeral ports
	defConfig.DMR.MMDVM.Bind = "127.0.0.1"
	defConfig.DMR.MMDVM.Port = 0

	defConfig.DMR.IPSC.Enabled = true
	defConfig.DMR.IPSC.Bind = "127.0.0.1"
	defConfig.DMR.IPSC.Port = 0
	defConfig.DMR.IPSC.NetworkID = 999999

	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	ps, err := pubsub.MakePubSub(context.Background(), &defConfig)
	require.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

	ct := calltracker.NewCallTracker(database, ps)

	h := hub.NewHub(database, kvStore, ps, ct)

	// Create servers
	mmdvmServer, err := mmdvm.MakeServer(&defConfig, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)

	ipscServer := ipsc.NewIPSCServer(&defConfig, h, database)

	stack := &IntegrationStack{
		Config:      &defConfig,
		DB:          database,
		Hub:         h,
		PubSub:      ps,
		KV:          kvStore,
		CallTracker: ct,
		MMDVMServer: &mmdvmServer,
		IPSCServer:  ipscServer,
	}

	t.Cleanup(func() {
		_ = ipscServer.Stop(context.Background())
		_ = mmdvmServer.Stop(context.Background())
		h.Stop(context.Background())
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = kvStore.Close()
	})

	return stack
}

// SpawnSecondReplica creates a second Hub + MMDVM server that shares the same
// DB, pubsub, and KV as the original stack — simulating two replicas of the app.
// The second replica's Hub is started; repeater subscriptions are activated
// when repeaters connect via the MMDVM handshake.
// Returns the second MMDVM server's listen address.
func (s *IntegrationStack) SpawnSecondReplica(t *testing.T) string {
	t.Helper()

	ct2 := calltracker.NewCallTracker(s.DB, s.PubSub)
	hub2 := hub.NewHub(s.DB, s.KV, s.PubSub, ct2)

	// Use a separate config with port=0 for the second server
	cfg2 := *s.Config
	cfg2.DMR.MMDVM.Port = 0

	mmdvm2, err := mmdvm.MakeServer(&cfg2, hub2, s.DB, s.PubSub, s.KV, "test", "test")
	require.NoError(t, err)

	err = mmdvm2.Start(context.Background())
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = mmdvm2.Stop(context.Background())
		hub2.Stop(context.Background())
	})

	return mmdvm2.Server.LocalAddr().String()
}

// StartServers starts the Hub and the MMDVM and IPSC servers.
// Repeater subscriptions are activated lazily when repeaters connect
// via the protocol handshake.
func (s *IntegrationStack) StartServers(t *testing.T) {
	t.Helper()

	// Start servers — they register with the hub and begin listening
	err := s.MMDVMServer.Start(context.Background())
	require.NoError(t, err)

	err = s.IPSCServer.Start(context.Background())
	require.NoError(t, err)

	s.MMDVMAddr = s.MMDVMServer.Server.LocalAddr().String()
	s.IPSCAddr = s.IPSCServer.Addr().String()
}

// SeedUser creates a User in the DB.
func (s *IntegrationStack) SeedUser(t *testing.T, id uint, callsign string) {
	t.Helper()
	err := s.DB.Create(&models.User{
		ID:       id,
		Callsign: callsign,
		Username: callsign,
		Approved: true,
	}).Error
	require.NoError(t, err)
}

// SeedTalkgroup creates a Talkgroup in the DB.
func (s *IntegrationStack) SeedTalkgroup(t *testing.T, id uint, name string) {
	t.Helper()
	err := s.DB.Create(&models.Talkgroup{
		ID:   id,
		Name: name,
	}).Error
	require.NoError(t, err)
}

// SeedMMDVMRepeater creates an MMDVM repeater in the DB.
func (s *IntegrationStack) SeedMMDVMRepeater(t *testing.T, id uint, ownerID uint, password string) {
	t.Helper()
	err := s.DB.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{
			Callsign: fmt.Sprintf("T%04dST", id%10000),
			ID:       id,
		},
		OwnerID:  ownerID,
		Type:     models.RepeaterTypeMMDVM,
		Password: password,
	}).Error
	require.NoError(t, err)
}

// SeedIPSCRepeater creates an IPSC repeater in the DB.
func (s *IntegrationStack) SeedIPSCRepeater(t *testing.T, id uint, ownerID uint, password string) {
	t.Helper()
	err := s.DB.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{
			Callsign: fmt.Sprintf("I%04dST", id%10000),
			ID:       id,
		},
		OwnerID:  ownerID,
		Type:     models.RepeaterTypeIPSC,
		Password: password,
	}).Error
	require.NoError(t, err)
}

// AssignTS1StaticTG adds a talkgroup to a repeater's TS1 static list.
func (s *IntegrationStack) AssignTS1StaticTG(t *testing.T, repeaterID, tgID uint) {
	t.Helper()
	var rpt models.Repeater
	require.NoError(t, s.DB.First(&rpt, repeaterID).Error)
	require.NoError(t, s.DB.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: tgID}))
}

// AssignTS2StaticTG adds a talkgroup to a repeater's TS2 static list.
func (s *IntegrationStack) AssignTS2StaticTG(t *testing.T, repeaterID, tgID uint) {
	t.Helper()
	var rpt models.Repeater
	require.NoError(t, s.DB.First(&rpt, repeaterID).Error)
	require.NoError(t, s.DB.Model(&rpt).Association("TS2StaticTalkgroups").Append(&models.Talkgroup{ID: tgID}))
}
