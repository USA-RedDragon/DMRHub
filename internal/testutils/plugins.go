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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// pluginSecret is the secret that the plugin certificate of the tests
// comes from.
const pluginSecret = "a secret of the config, long enough"

// Plugins is a plugin listener on loopback for a test.
type Plugins struct {
	cfg    *config.Config
	db     *gorm.DB
	ps     pubsub.PubSub
	Server *plugins.Server
}

// StartPlugins starts the plugin listener of router on loopback until the
// test ends. notify gets the positions the hub stores; it may be nil.
func StartPlugins(t testing.TB, db *gorm.DB, ps pubsub.PubSub, router plugins.Router, setPlugins func(*plugins.Registry),
	notify func(models.Position)) *Plugins {
	t.Helper()
	var cfg config.Config
	cfg.Secret = pluginSecret
	cfg.DMR.Plugins = config.Plugins{Enabled: true, Bind: "127.0.0.1", Port: 0}
	srv, err := plugins.Start(context.Background(), &cfg, db, ps, router, setPlugins, notify)
	require.NoError(t, err)
	t.Cleanup(srv.Stop)
	return &Plugins{cfg: &cfg, db: db, ps: ps, Server: srv}
}

// Run adds the plugin name with ranges and runs serve on its connection,
// as its command would, until the test ends. It returns once the plugin is
// connected.
func (p *Plugins) Run(t testing.TB, name string, ranges []models.PluginRange, serve func(context.Context, *plugin.Conn) error) models.Plugin {
	t.Helper()
	added, token, err := models.CreatePlugin(p.db, name, ranges, time.Now())
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(p.ps))
	require.Eventually(t, func() bool {
		owner, ok := p.Server.Registry().Owner(ranges[0].First)
		return ok && owner == added.ID
	}, 5*time.Second, 10*time.Millisecond, "the hub learns the IDs of %s", name)
	certs, err := plugins.NewCertSource(p.cfg.GetDerivedSecret(), "", "")
	require.NoError(t, err)
	fp, err := certs.Fingerprint()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		<-done
	})
	go func() {
		defer close(done)
		plugin.Run(ctx, plugin.Options{Address: p.Server.Addr().String(), Token: token, Fingerprint: fp}, serve)
	}()
	require.Eventually(t, func() bool {
		got, err := models.FindPlugin(p.db, added.ID)
		return err == nil && got.Online(time.Now())
	}, 5*time.Second, 10*time.Millisecond, "the plugin %s connects", name)
	return added
}

// RepeaterHub returns a hub on an in-memory database, with the user owner
// and the MMDVM repeater it owns, active, and the server handle that gets
// what the hub sends to the repeater. It stops when the test ends.
func RepeaterHub(t testing.TB, owner, repeater uint) (*hub.Hub, *gorm.DB, pubsub.PubSub, *hub.ServerHandle) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_repeaterhub_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, calltracker.NewCallTracker(database, ps, kvStore))
	t.Cleanup(func() {
		h.Stop(context.Background())
		_ = ps.Close()
		_ = kvStore.Close()
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, database.Create(&models.User{ID: owner, Callsign: "OWNER", Username: "owner", Approved: true}).Error)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{Callsign: "T1ST", ID: repeater, ColorCode: 3},
		OwnerID:               owner,
		Type:                  models.RepeaterTypeMMDVM,
	}).Error)
	srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	h.ActivateRepeater(context.Background(), repeater)
	return h, database, ps, srv
}
