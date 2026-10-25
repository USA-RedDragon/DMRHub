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

package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type noRouter struct{}

func (noRouter) RoutePacket(context.Context, models.Packet, string) {}

// drainRig is a serverManager with a plugin listener and one plugin stream
// on it.
func drainRig(t *testing.T, otherReplica bool) (*serverManager, <-chan *pluginv1.ConnectResponse) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Secret = "a secret of the config, long enough"
	cfg.Database.Database = fmt.Sprintf("file:memdb_cmddrain_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	cfg.DMR.Plugins = config.Plugins{Enabled: true, Bind: "127.0.0.1", Port: 0}
	cfg.DMR.DrainTimeout = 1
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = ps.Close()
		_ = store.Close()
	})
	if otherReplica {
		servers.NewInstanceRegistry(ctx, store, "other")
	}
	_, token, err := models.CreatePlugin(database, "Parrot", []models.PluginRange{{First: 9990, Last: 9990}}, time.Now())
	require.NoError(t, err)
	srv, err := plugins.Start(ctx, &cfg, database, ps, noRouter{}, func(*plugins.Registry) {}, nil)
	require.NoError(t, err)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(srv.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))) //nolint:gosec // the test's own listener
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	stream, err := pluginv1.NewPluginServiceClient(conn).Connect(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token))
	require.NoError(t, err)
	first, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, first.GetWelcome())
	msgs := make(chan *pluginv1.ConnectResponse, 8)
	go func() {
		for {
			m, err := stream.Recv()
			if err != nil {
				return
			}
			msgs <- m
		}
	}()
	sm := &serverManager{
		dmr: manager.New(manager.Deps{}), plugins: srv, cfg: &cfg,
		registry: servers.NewInstanceRegistry(ctx, store, "self"),
	}
	return sm, msgs
}

func TestTheDrainMovesThePluginsWhenAnotherReplicaRuns(t *testing.T) {
	t.Parallel()
	sm, msgs := drainRig(t, true)
	go sm.drain(context.Background())
	select {
	case m := <-msgs:
		require.NotNil(t, m.GetDraining(), "got %v", m)
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin did not learn that the replica stops")
	}
}

func TestTheDrainLeavesThePluginsWhenNoOtherReplicaRuns(t *testing.T) {
	t.Parallel()
	sm, msgs := drainRig(t, false)
	sm.drain(context.Background())
	select {
	case m := <-msgs:
		t.Fatalf("the plugin got %v from a lone replica", m)
	case <-time.After(300 * time.Millisecond):
	}
}
