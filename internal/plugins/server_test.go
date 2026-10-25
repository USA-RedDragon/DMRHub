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

package plugins_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPluginConnectsOverTLSByTheFingerprint(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Secret = "a secret of the config, long enough"
	cfg.Database.Database = fmt.Sprintf("file:memdb_pluginsrv_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	cfg.DMR.Plugins = config.Plugins{Enabled: true, Bind: "127.0.0.1", Port: 0}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })

	p, token, err := models.CreatePlugin(database, "Parrot", []models.PluginRange{{First: 9990, Last: 9990}}, time.Now())
	require.NoError(t, err)
	var given *plugins.Registry
	srv, err := plugins.Start(context.Background(), &cfg, database, ps, &routed{}, func(r *plugins.Registry) { given = r }, nil)
	require.NoError(t, err)
	t.Cleanup(srv.Stop)
	require.NotNil(t, given)
	certs, err := plugins.NewCertSource(cfg.GetDerivedSecret(), "", "")
	require.NoError(t, err)
	fp, err := certs.Fingerprint()
	require.NoError(t, err)
	addr := srv.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = plugin.Dial(ctx, plugin.Options{Address: addr, Token: token})
	require.Error(t, err, "a certificate of its own is not trusted without its fingerprint")
	_, err = plugin.Dial(ctx, plugin.Options{Address: addr, Token: token, Fingerprint: "00:11"})
	require.Error(t, err)

	conn, err := plugin.Dial(ctx, plugin.Options{Address: addr, Token: token, Fingerprint: fp})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	assert.Equal(t, "Parrot", conn.Name())
	require.Len(t, conn.Ranges(), 1)

	given.Deliver(p.ID, models.Packet{Src: 3191868, Dst: 9990, StreamID: 9})
	m, err := conn.Recv()
	require.NoError(t, err)
	assert.Equal(t, uint32(3191868), m.GetBurst().GetSource())
}

func TestAListenerThatIsOffStartsNothing(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.DMR.Plugins.InProcess = false
	called := false
	srv, err := plugins.Start(context.Background(), &cfg, nil, nil, nil, func(*plugins.Registry) { called = true }, nil)
	require.NoError(t, err)
	assert.Nil(t, srv)
	assert.False(t, called, "the built-in services keep their IDs")
	srv.Stop()
}

func TestStoppingTheListenerTwiceIsSafe(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Secret = "a secret of the config, long enough"
	cfg.Database.Database = fmt.Sprintf("file:memdb_pluginstop_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	cfg.DMR.Plugins = config.Plugins{Enabled: true, Bind: "127.0.0.1", Port: 0}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	srv, err := plugins.Start(context.Background(), &cfg, database, ps, &routed{}, func(*plugins.Registry) {}, nil)
	require.NoError(t, err)
	srv.Stop()
	assert.NotPanics(t, srv.Stop, "the server shuts down its resources twice when the HTTP server cannot start")
}
