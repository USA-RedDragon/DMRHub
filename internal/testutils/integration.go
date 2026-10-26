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
	"net"
	"strconv"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/ipsc"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/mmdvm"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// IntegrationStack is a full DMRHub test environment with real MMDVM and IPSC
// servers on ephemeral UDP ports, backed by in-memory SQLite, pubsub, and KV.
type IntegrationStack struct {
	Config           *config.Config
	DB               *gorm.DB
	Hub              *hub.Hub
	PubSub           pubsub.PubSub
	KV               kv.KV
	CallTracker      *calltracker.CallTracker
	MessageStore     *messages.Store
	MMDVMServer      *mmdvm.Server
	IPSCServer       *ipsc.IPSCServer
	OpenBridgeServer *openbridge.Server
	MMDVMAddr        string // host:port of the MMDVM server
	IPSCAddr         string // host:port of the IPSC server
	OpenBridgeAddr   string // host:port of the OpenBridge server
	// WebTransportAddr is the host:port of the WebTransport listener of
	// the MMDVM server, empty when it is off.
	WebTransportAddr string

	plugins *Plugins
}

// SetupIntegrationStack creates and starts a full integration test environment.
// The hub is NOT started yet — call SeedAndStart() after seeding your DB data.
func SetupIntegrationStack(t *testing.T, backends ...Backend) *IntegrationStack {
	t.Helper()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
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

	const localhost = "127.0.0.1"

	// Ephemeral ports
	defConfig.DMR.MMDVM.Bind = localhost
	defConfig.DMR.MMDVM.Port = 0

	defConfig.DMR.IPSC.Enabled = true
	defConfig.DMR.IPSC.Bind = localhost
	defConfig.DMR.IPSC.Port = 0
	defConfig.DMR.IPSC.NetworkID = 999999

	defConfig.DMR.OpenBridge.Enabled = true
	defConfig.DMR.OpenBridge.Bind = localhost
	defConfig.DMR.OpenBridge.Port = 0

	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	ps, err := pubsub.MakePubSub(context.Background(), &defConfig)
	require.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

	for _, be := range backends {
		if be.WrapKV != nil {
			kvStore = be.WrapKV(kvStore)
		}
		if be.WrapPubSub != nil {
			ps = be.WrapPubSub(ps)
		}
	}

	ct := calltracker.NewCallTracker(database, ps, kvStore)
	store := messages.NewStore(database, kvStore)
	store.SetNotify(messages.NewPusher(database, ps).Push)
	ct.SetDataObserver(store)

	h := hub.NewHub(database, kvStore, ps, ct)

	// Create servers
	mmdvmServer, err := mmdvm.MakeServer(&defConfig, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)

	ipscServer := ipsc.NewIPSCServer(&defConfig, h, database)

	obServer, err := openbridge.MakeServer(&defConfig, h, database, ps, kvStore)
	require.NoError(t, err)

	stack := &IntegrationStack{
		Config:           &defConfig,
		DB:               database,
		Hub:              h,
		PubSub:           ps,
		KV:               kvStore,
		CallTracker:      ct,
		MessageStore:     store,
		MMDVMServer:      &mmdvmServer,
		IPSCServer:       ipscServer,
		OpenBridgeServer: &obServer,
	}

	t.Cleanup(func() {
		_ = obServer.Stop(context.Background())
		_ = ipscServer.Stop(context.Background())
		_ = stack.MMDVMServer.Stop(context.Background())
		h.Stop(context.Background())
		ct.Stop()
		store.Stop()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = kvStore.Close()
	})

	return stack
}

// Replica is a second running copy of the app made by SpawnSecondReplica,
// with its own Hub, call tracker and MMDVM, IPSC and OpenBridge servers.
type Replica struct {
	Hub              *hub.Hub
	CallTracker      *calltracker.CallTracker
	MMDVMServer      *mmdvm.Server
	IPSCServer       *ipsc.IPSCServer
	OpenBridgeServer *openbridge.Server
	MMDVMAddr        string
	IPSCAddr         string
	OpenBridgeAddr   string
	WebTransportAddr string
}

// SpawnSecondReplica creates a second Hub with MMDVM, IPSC and OpenBridge
// servers that share the same DB, pubsub, and KV as the original stack,
// simulating two replicas of the app. The servers listen on new ports and
// are started; repeater subscriptions are activated when repeaters connect.
func (s *IntegrationStack) SpawnSecondReplica(t *testing.T) Replica {
	t.Helper()
	r, _ := s.spawnReplica(t, false)
	return r
}

// SpawnRegisteredReplica starts a second replica, as SpawnSecondReplica
// does, and gives its hub a replica ID with Register before its servers
// start. It returns the replica and its registry.
func (s *IntegrationStack) SpawnRegisteredReplica(t *testing.T) (*Replica, *servers.InstanceRegistry) {
	t.Helper()
	r, reg := s.spawnReplica(t, true)
	return &r, reg
}

func (s *IntegrationStack) spawnReplica(t *testing.T, register bool) (Replica, *servers.InstanceRegistry) {
	t.Helper()

	ct2 := calltracker.NewCallTracker(s.DB, s.PubSub, s.KV)
	store2 := messages.NewStore(s.DB, s.KV)
	store2.SetNotify(messages.NewPusher(s.DB, s.PubSub).Push)
	ct2.SetDataObserver(store2)
	hub2 := hub.NewHub(s.DB, s.KV, s.PubSub, ct2)
	var reg *servers.InstanceRegistry
	if register {
		reg = Register(t, hub2, s.KV)
	}

	cfg2 := *s.Config
	cfg2.DMR.MMDVM.Port = 0
	cfg2.DMR.MMDVM.WebTransport.Port = 0
	cfg2.DMR.IPSC.Port = 0
	cfg2.DMR.OpenBridge.Port = 0

	mmdvm2, err := mmdvm.MakeServer(&cfg2, hub2, s.DB, s.PubSub, s.KV, "test", "test")
	require.NoError(t, err)
	require.NoError(t, mmdvm2.Start(context.Background()))

	ipsc2 := ipsc.NewIPSCServer(&cfg2, hub2, s.DB)
	require.NoError(t, ipsc2.Start(context.Background()))

	ob2, err := openbridge.MakeServer(&cfg2, hub2, s.DB, s.PubSub, s.KV)
	require.NoError(t, err)
	require.NoError(t, ob2.Start(context.Background()))

	t.Cleanup(func() {
		_ = ob2.Stop(context.Background())
		_ = ipsc2.Stop(context.Background())
		_ = mmdvm2.Stop(context.Background())
		hub2.Stop(context.Background())
		ct2.Stop()
		store2.Stop()
	})

	return Replica{
		Hub:              hub2,
		CallTracker:      ct2,
		MMDVMServer:      &mmdvm2,
		IPSCServer:       ipsc2,
		OpenBridgeServer: &ob2,
		MMDVMAddr:        mmdvm2.Server.LocalAddr().String(),
		IPSCAddr:         ipsc2.Addr().String(),
		OpenBridgeAddr:   ob2.Server.LocalAddr().String(),
		WebTransportAddr: addrOf(mmdvm2.WebTransportAddr()),
	}, reg
}

// Replica returns the first replica of the stack, the one that
// SetupIntegrationStack made, as a Replica.
func (s *IntegrationStack) Replica() *Replica {
	return &Replica{
		Hub:              s.Hub,
		CallTracker:      s.CallTracker,
		MMDVMServer:      s.MMDVMServer,
		IPSCServer:       s.IPSCServer,
		OpenBridgeServer: s.OpenBridgeServer,
		MMDVMAddr:        s.MMDVMAddr,
		IPSCAddr:         s.IPSCAddr,
		OpenBridgeAddr:   s.OpenBridgeAddr,
		WebTransportAddr: s.WebTransportAddr,
	}
}

func addrOf(a net.Addr) string {
	if a == nil {
		return ""
	}
	return a.String()
}

// SpawnFullReplica starts a second replica, as SpawnSecondReplica does, and
// returns it.
func (s *IntegrationStack) SpawnFullReplica(t *testing.T) *Replica {
	t.Helper()
	r := s.SpawnSecondReplica(t)
	return &r
}

// RestartMode is how RestartMMDVM stops the MMDVM server.
type RestartMode int

const (
	// RestartClose stops the server as a lone instance does: it sends MSTCL
	// to its repeaters, which log in again.
	RestartClose RestartMode = iota
	// RestartHandoff stops the server as when another replica is alive: it
	// sends nothing, and the new server resumes the sessions from the KV
	// records.
	RestartHandoff
	// RestartLost stops the server without MSTCL and drops the KV records
	// of the repeaters, as after a crash, so the new server answers their
	// pings with MSTNAK.
	RestartLost
)

// RestartMMDVM stops the MMDVM server as mode says and starts a new one on
// the same address, as a restart of the hub does. The stack stops the new
// server at the end of the test.
func (s *IntegrationStack) RestartMMDVM(t *testing.T, mode RestartMode) {
	t.Helper()
	ctx := servers.WithGracefulHandoff(context.Background(), mode != RestartClose)
	require.NoError(t, s.MMDVMServer.Stop(ctx))
	if mode == RestartLost {
		kv := servers.MakeKVClient(s.KV)
		ids, err := kv.ListRepeaters(ctx)
		require.NoError(t, err)
		for _, id := range ids {
			kv.DeleteRepeater(ctx, id)
		}
	}

	_, port, err := net.SplitHostPort(s.MMDVMAddr)
	require.NoError(t, err)
	cfg := *s.Config
	cfg.DMR.MMDVM.Port, err = strconv.Atoi(port)
	require.NoError(t, err)
	m, err := mmdvm.MakeServer(&cfg, s.Hub, s.DB, s.PubSub, s.KV, "test", "test")
	require.NoError(t, err)
	require.NoError(t, m.Start(context.Background()))
	s.MMDVMServer = &m
}

// StartServers starts the Hub and the MMDVM, IPSC, and OpenBridge servers.
// Repeater subscriptions are activated lazily when repeaters connect
// via the protocol handshake.
func (s *IntegrationStack) StartServers(t *testing.T) {
	t.Helper()

	// Start servers — they register with the hub and begin listening
	err := s.MMDVMServer.Start(context.Background())
	require.NoError(t, err)

	err = s.IPSCServer.Start(context.Background())
	require.NoError(t, err)

	err = s.OpenBridgeServer.Start(context.Background())
	require.NoError(t, err)

	s.MMDVMAddr = s.MMDVMServer.Server.LocalAddr().String()
	s.IPSCAddr = s.IPSCServer.Addr().String()
	s.OpenBridgeAddr = s.OpenBridgeServer.Server.LocalAddr().String()
	s.WebTransportAddr = addrOf(s.MMDVMServer.WebTransportAddr())
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

// SeedPeer creates an OpenBridge peer in the DB.
func (s *IntegrationStack) SeedPeer(t *testing.T, id uint, ownerID uint, password string, ip string, port int, ingress bool, egress bool) {
	t.Helper()
	err := s.DB.Create(&models.Peer{
		ID:       id,
		OwnerID:  ownerID,
		Password: password,
		IP:       ip,
		Port:     port,
		Ingress:  ingress,
		Egress:   egress,
	}).Error
	require.NoError(t, err)
}

// SeedPeerRule creates a peer rule in the DB.
func (s *IntegrationStack) SeedPeerRule(t *testing.T, peerID uint, direction bool, subjectIDMin uint, subjectIDMax uint) {
	t.Helper()
	err := s.DB.Create(&models.PeerRule{
		PeerID:       peerID,
		Direction:    direction,
		SubjectIDMin: subjectIDMin,
		SubjectIDMax: subjectIDMax,
	}).Error
	require.NoError(t, err)
}
