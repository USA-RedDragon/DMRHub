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
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
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
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
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
	return p.RunAt(t, p.Server.Addr().String(), name, ranges, serve)
}

// RunAt is Run with the plugin connecting to address, such as that of a
// Balancer in front of the listeners of several replicas.
func (p *Plugins) RunAt(t testing.TB, address, name string, ranges []models.PluginRange,
	serve func(context.Context, *plugin.Conn) error) models.Plugin {
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
		plugin.Run(ctx, plugin.Options{Address: address, Token: token, Fingerprint: fp}, serve)
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

// RunPlugin starts the plugin listener of the stack's hub, when it has not
// yet, and runs the plugin name with ranges on it, as RunPlugin of
// Plugins does.
func (s *IntegrationStack) RunPlugin(t testing.TB, name string, ranges []models.PluginRange, serve func(context.Context, *plugin.Conn) error) {
	t.Helper()
	s.Plugins(t).Run(t, name, ranges, serve)
}

// StartPlugins starts the plugin listener of the replica on loopback, on
// the stack's database and pubsub, until the test ends.
func (r *Replica) StartPlugins(t testing.TB, s *IntegrationStack) *Plugins {
	t.Helper()
	return StartPlugins(t, s.DB, s.PubSub, r.Hub, func(reg *plugins.Registry) { r.Hub.SetPlugins(reg) }, nil)
}

// Plugins returns the plugin listener of the stack's hub, and starts it
// when it has not yet.
func (s *IntegrationStack) Plugins(t testing.TB) *Plugins {
	t.Helper()
	if s.plugins == nil {
		s.plugins = StartPlugins(t, s.DB, s.PubSub, s.Hub, func(r *plugins.Registry) { s.Hub.SetPlugins(r) }, nil)
	}
	return s.plugins
}

// Balancer forwards each new TCP connection to the backend it has at that
// time, and keeps an open one on its backend, as a Kubernetes Service does
// with the ready pods.
type Balancer struct {
	listener net.Listener
	mu       sync.Mutex
	backend  string
}

// NewBalancer listens on loopback and forwards to backend until the test
// ends.
func NewBalancer(t testing.TB, backend string) *Balancer {
	t.Helper()
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	b := &Balancer{listener: lis, backend: backend}
	var wg sync.WaitGroup
	var open sync.Map
	t.Cleanup(func() {
		_ = lis.Close()
		open.Range(func(k, _ any) bool {
			if c, ok := k.(net.Conn); ok {
				_ = c.Close()
			}
			return true
		})
		wg.Wait()
	})
	wg.Go(func() {
		for {
			in, err := lis.Accept()
			if err != nil {
				return
			}
			var d net.Dialer
			out, err := d.DialContext(context.Background(), "tcp", b.Backend())
			if err != nil {
				_ = in.Close()
				continue
			}
			open.Store(in, struct{}{})
			open.Store(out, struct{}{})
			pipe := func(dst, src net.Conn) {
				_, _ = io.Copy(dst, src)
				_ = dst.Close()
				_ = src.Close()
				open.Delete(dst)
				open.Delete(src)
			}
			wg.Go(func() { pipe(out, in) })
			wg.Go(func() { pipe(in, out) })
		}
	})
	return b
}

// Addr returns the address the balancer listens on.
func (b *Balancer) Addr() string {
	return b.listener.Addr().String()
}

// Backend returns where new connections go.
func (b *Balancer) Backend() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.backend
}

// SetBackend sends the new connections to backend.
func (b *Balancer) SetBackend(backend string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.backend = backend
}

// FakePluginHub serves the plugin protocol with serve on loopback, over TLS
// with the certificate of the tests, until the test ends. It returns the
// options that reach it, with any token.
func FakePluginHub(t testing.TB, serve func(pluginv1.PluginService_ConnectServer) error) plugin.Options {
	t.Helper()
	var cfg config.Config
	cfg.Secret = pluginSecret
	certs, err := plugins.NewCertSource(cfg.GetDerivedSecret(), "", "")
	require.NoError(t, err)
	fp, err := certs.Fingerprint()
	require.NoError(t, err)
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{GetCertificate: certs.GetCertificate, MinVersion: tls.VersionTLS13})))
	pluginv1.RegisterPluginServiceServer(srv, fakeHub{serve: serve})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		<-done
	})
	return plugin.Options{Address: lis.Addr().String(), Token: "dmrhub_plg_test", Fingerprint: fp}
}

type fakeHub struct {
	pluginv1.UnimplementedPluginServiceServer
	serve func(pluginv1.PluginService_ConnectServer) error
}

func (h fakeHub) Connect(stream pluginv1.PluginService_ConnectServer) error {
	return h.serve(stream)
}

// ServeIgnoresUnknownMessages checks that serve keeps the connection when
// the hub sends a message with no type that the plugin knows, as an SDK
// decodes a message of a newer hub.
func ServeIgnoresUnknownMessages(t *testing.T, serve func(context.Context, *plugin.Conn) error) {
	t.Helper()
	end := make(chan struct{})
	opts := FakePluginHub(t, func(s pluginv1.PluginService_ConnectServer) error {
		if err := s.Send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Welcome{Welcome: &pluginv1.Welcome{
			Name: "P", Ranges: []*pluginv1.IDRange{{First: 9990, Last: 9990}},
		}}}); err != nil {
			return err //nolint:wrapcheck // the test's hub
		}
		if err := s.Send(&pluginv1.ConnectResponse{}); err != nil {
			return err //nolint:wrapcheck // the test's hub
		}
		<-end
		return status.Error(codes.Unavailable, "the hub stops")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, err := plugin.Dial(ctx, opts)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, conn) }()
	select {
	case err := <-done:
		close(end)
		t.Fatalf("serve ended on a message it does not know: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	close(end)
	select {
	case err := <-done:
		require.Equal(t, codes.Unavailable, status.Code(err), "serve ends with the stream: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not end with the stream")
	}
}
