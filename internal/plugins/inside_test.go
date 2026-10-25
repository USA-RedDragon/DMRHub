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
	"io"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// innerStream is the stream of a plugin inside DMRHub, for a test.
type innerStream struct {
	ctx context.Context //nolint:containedctx // the context of the stream
	in  chan *pluginv1.ConnectRequest
	out chan *pluginv1.ConnectResponse
}

func newInnerStream(ctx context.Context) *innerStream {
	return &innerStream{ctx: ctx, in: make(chan *pluginv1.ConnectRequest, 16), out: make(chan *pluginv1.ConnectResponse, 16)}
}

func (s *innerStream) Send(m *pluginv1.ConnectResponse) error { s.out <- m; return nil }
func (s *innerStream) Recv() (*pluginv1.ConnectRequest, error) {
	select {
	case m := <-s.in:
		return m, nil
	case <-s.ctx.Done():
		return nil, io.EOF
	}
}
func (s *innerStream) Context() context.Context     { return s.ctx }
func (s *innerStream) SetHeader(metadata.MD) error  { return nil }
func (s *innerStream) SendHeader(metadata.MD) error { return nil }
func (s *innerStream) SetTrailer(metadata.MD)       {}
func (s *innerStream) SendMsg(any) error            { return nil }
func (s *innerStream) RecvMsg(any) error            { return nil }

func uploaded(t *testing.T, r *rig, name string, ranges ...models.PluginRange) models.Plugin {
	t.Helper()
	p, err := models.CreateUploadedPlugin(r.db, models.PluginInfo{Name: name, Version: "0.4.1"}, ranges, nil, []byte("wasm"), time.Now())
	require.NoError(t, err)
	require.NoError(t, r.registry.Reload())
	return p
}

func TestAPluginInsideDMRHubConnectsWithoutAToken(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p := uploaded(t, r, "Net check-in", rng(9100, 9100))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newInnerStream(plugins.InProcess(ctx, p.ID))
	done := make(chan error, 1)
	go func() { done <- r.svc.Connect(s) }()
	select {
	case m := <-s.out:
		require.NotNil(t, m.GetWelcome())
		assert.Equal(t, "Net check-in", m.GetWelcome().GetName())
		require.Len(t, m.GetWelcome().GetRanges(), 1)
	case err := <-done:
		t.Fatalf("Connect ended with %v before the Welcome", err)
	case <-time.After(3 * time.Second):
		t.Fatal("no Welcome")
	}
	r.registry.Deliver(p.ID, models.Packet{Src: 3191868, Dst: 9100, StreamID: 4})
	select {
	case m := <-s.out:
		assert.Equal(t, uint32(9100), m.GetBurst().GetDestination())
	case <-time.After(3 * time.Second):
		t.Fatal("the burst did not reach the plugin")
	}
	cancel()
	<-done
}

func TestAnExternalPluginCannotConnectAsOneInside(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, _ := r.add("Weather", rng(310001, 310009))
	s := newInnerStream(plugins.InProcess(context.Background(), p.ID))
	err := r.svc.Connect(s)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	s = newInnerStream(plugins.InProcess(context.Background(), 999))
	err = r.svc.Connect(s)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAPluginInsideDMRHubHasNoTokenToConnectWith(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	uploaded(t, r, "Net check-in", rng(9100, 9100))
	_, err := recv(t, r.connect(""))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestTheHubNotesTheVersionAnExternalPluginSays(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Weather", rng(310001, 310009))
	s := r.connect(token)
	_, err := recv(t, s)
	require.NoError(t, err)
	require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Hello{Hello: &pluginv1.Hello{Name: "ignored", Version: "2.0.0"}}}))
	require.Eventually(t, func() bool {
		got, err := models.FindPlugin(r.db, p.ID)
		return err == nil && got.Version == "2.0.0" && got.Name == "Weather"
	}, 3*time.Second, 10*time.Millisecond)
}

func TestTheRegistryLeavesOutThePluginsInsideWhenTheyAreOff(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p := uploaded(t, r, "Net check-in", rng(9100, 9100))
	ext, _ := r.add("Weather", rng(310001, 310009))
	owner, ok := r.registry.Owner(9100)
	require.True(t, ok, "plugins run inside DMRHub by default")
	assert.Equal(t, p.ID, owner)
	require.NoError(t, r.registry.SetInProcess(false))
	_, ok = r.registry.Owner(9100)
	assert.False(t, ok, "nobody runs the plugins inside DMRHub when they are off")
	owner, ok = r.registry.Owner(310005)
	assert.True(t, ok)
	assert.Equal(t, ext.ID, owner)
	require.NoError(t, r.registry.SetInProcess(true))
	_, ok = r.registry.Owner(9100)
	assert.True(t, ok)
}

func TestPluginsInsideDMRHubNeedNoListener(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_pluginsinside_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	require.False(t, cfg.DMR.Plugins.Enabled)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	var given *plugins.Registry
	srv, err := plugins.Start(context.Background(), &cfg, database, ps, &routed{}, func(r *plugins.Registry) { given = r }, nil)
	require.NoError(t, err)
	t.Cleanup(srv.Stop)
	require.NotNil(t, srv)
	assert.NotNil(t, given, "the hub routes to the plugins inside DMRHub")
	assert.NotNil(t, srv.Service())
	assert.Nil(t, srv.Addr(), "nothing listens")
}
