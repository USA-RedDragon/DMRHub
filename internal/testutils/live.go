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
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// LiveStack is an IntegrationStack whose DMR servers a manager.Manager
// runs, with an HTTP router whose config API saves the Setup settings with
// a liveconfig.Live that puts them into effect on that manager, and an
// admin session. More replicas that share its database, pubsub and KV
// store come from SpawnLiveReplica.
type LiveStack struct {
	*IntegrationStack
	Manager *manager.Manager
	Live    *liveconfig.Live
	Router  *gin.Engine
	Admin   CookieJar
	first   *LiveReplica
}

// LiveReplica is one replica of a LiveStack: a hub whose servers its
// Manager runs, its Live and its router, with an admin session.
type LiveReplica struct {
	Hub     *hub.Hub
	Config  *config.Config
	Manager *manager.Manager
	Live    *liveconfig.Live
	Router  *gin.Engine
	Admin   CookieJar
}

// LiveOptions change a replica of a LiveStack.
type LiveOptions struct {
	// Change changes the config of the replica before it starts, as its
	// own config file or environment would.
	Change func(*config.Config)
	// Locks are the settings that the config file, the environment or the
	// flags of the replica set.
	Locks liveconfig.Locks
	// Every and Wait are those of liveconfig.Replica; 0 is 200
	// milliseconds and 5 seconds.
	Every, Wait time.Duration
}

// FreeUDPPort returns a loopback UDP port that was free a moment ago.
func FreeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	addr, ok := c.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	return addr.Port
}

// SetupLiveStack makes a LiveStack whose MMDVM, IPSC and OpenBridge
// servers listen on free loopback ports. The servers of the
// IntegrationStack itself never start.
func SetupLiveStack(t *testing.T, backends ...Backend) *LiveStack {
	t.Helper()
	return setupLiveStack(t, false, LiveOptions{}, backends)
}

// SetupRegisteredLiveStack makes a LiveStack, as SetupLiveStack does, and
// gives its hub a replica ID with Register before its servers start.
func SetupRegisteredLiveStack(t *testing.T, backends ...Backend) *LiveStack {
	t.Helper()
	return setupLiveStack(t, true, LiveOptions{}, backends)
}

// SetupLiveStackWith makes a LiveStack, as SetupLiveStack does, whose
// first replica opts change.
func SetupLiveStackWith(t *testing.T, opts LiveOptions, backends ...Backend) *LiveStack {
	t.Helper()
	return setupLiveStack(t, false, opts, backends)
}

func setupLiveStack(t *testing.T, register bool, opts LiveOptions, backends []Backend) *LiveStack {
	t.Helper()
	stack := SetupIntegrationStack(t, backends...)
	if register {
		Register(t, stack.Hub, stack.KV)
	}
	cfg := stack.Config
	cfg.Secret = "live-config-test-secret"
	cfg.PasswordSalt = "live-config-test-salt"
	cfg.HTTP.CanonicalHost = "http://localhost:3005"
	cfg.DMR.MMDVM.Port = FreeUDPPort(t)
	cfg.DMR.IPSC.Port = FreeUDPPort(t)
	cfg.DMR.OpenBridge.Port = FreeUDPPort(t)

	hash, err := utils.HashPassword(TestPassword, cfg.PasswordSalt)
	require.NoError(t, err)
	require.NoError(t, stack.DB.Create(&models.User{ID: 9999901, Callsign: "N0ADM", Username: "liveadmin", Password: hash, Approved: true, Admin: true, SuperAdmin: true}).Error)

	l := &LiveStack{IntegrationStack: stack}
	l.first = l.startReplica(t, stack.Hub, cfg, opts)
	l.Manager, l.Live, l.Router, l.Admin = l.first.Manager, l.first.Live, l.first.Router, l.first.Admin
	return l
}

// SpawnLiveReplica starts another replica that shares the database, the
// pubsub and the KV store of the stack, with its own hub, servers, Live
// and router. Its servers listen on free loopback ports, which its
// environment sets, so that they are locked, unless opts.Locks names the
// locked settings itself.
func (l *LiveStack) SpawnLiveReplica(t *testing.T, opts LiveOptions) *LiveReplica {
	t.Helper()
	ct := calltracker.NewCallTracker(l.DB, l.PubSub, l.KV)
	store := messages.NewStore(l.DB, l.KV)
	store.SetNotify(messages.NewPusher(l.DB, l.PubSub).Push)
	ct.SetDataObserver(store)
	h := hub.NewHub(l.DB, l.KV, l.PubSub, ct)
	Register(t, h, l.KV)
	t.Cleanup(func() {
		h.Stop(context.Background())
		ct.Stop()
		store.Stop()
	})

	cfg := *l.first.Config
	cfg.DMR.MMDVM.Port = FreeUDPPort(t)
	cfg.DMR.IPSC.Port = FreeUDPPort(t)
	cfg.DMR.OpenBridge.Port = FreeUDPPort(t)
	if opts.Locks == nil {
		opts.Locks = liveconfig.Locks{}
		for _, p := range []string{"dmr.mmdvm.port", "dmr.ipsc.port", "dmr.openbridge.port"} {
			opts.Locks[p] = liveconfig.Lock{Source: liveconfig.SourceEnv}
		}
	}
	return l.startReplica(t, h, &cfg, opts)
}

type managerApplier struct {
	m *manager.Manager
}

func (a managerApplier) Apply(next *config.Config) []liveconfig.ApplyError {
	return manager.LiveErrors(a.m.Apply(next))
}

func (l *LiveStack) startReplica(t *testing.T, h *hub.Hub, cfg *config.Config, opts LiveOptions) *LiveReplica {
	t.Helper()
	if opts.Change != nil {
		opts.Change(cfg)
	}
	live := liveconfig.New(liveconfig.Options{Base: cfg, Locks: opts.Locks, DB: l.DB})
	require.NoError(t, live.Boot(context.Background()))
	h.SetConfig(live.Current())

	m := manager.New(manager.Deps{Hub: h, DB: l.DB, PubSub: l.PubSub, KV: l.KV, Version: "test", Commit: "test"})
	require.NoError(t, m.Start(context.Background(), live.Current().Get()))
	t.Cleanup(func() { m.Stop(context.Background()) })

	id, err := servers.GenerateInstanceID()
	require.NoError(t, err)
	every := opts.Every
	if every == 0 {
		every = 200 * time.Millisecond
	}
	live.Start(context.Background(), liveconfig.Replica{ID: id, PubSub: l.PubSub, KV: l.KV, Applier: managerApplier{m}, Every: every, Wait: opts.Wait})
	t.Cleanup(func() { live.Stop(context.Background()) })

	ready := &atomic.Bool{}
	ready.Store(true)
	router := internalhttp.CreateRouter(context.Background(), cfg, h, l.DB, l.PubSub, ready, nil, "test", "test", internalhttp.WithLiveConfig(live))
	return &LiveReplica{Hub: h, Config: cfg, Manager: m, Live: live, Router: router, Admin: Login(t, router, "liveadmin")}
}

// Addr returns the host:port of server k.
func (l *LiveReplica) Addr(t *testing.T, k manager.Kind) string {
	t.Helper()
	a := l.Manager.Addr(k)
	require.NotNil(t, a, "the %s server is not running", k)
	return a.String()
}

// GetConfig returns the config that the config API serves.
func (l *LiveReplica) GetConfig(t *testing.T) map[string]any {
	t.Helper()
	w := Do(t, l.Router, l.Admin, http.MethodGet, "/api/v1/config", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	return cfg
}

// SetDMR changes the settings of server k in the config that the config
// API serves and saves the config with the API.
func (l *LiveReplica) SetDMR(t *testing.T, k manager.Kind, change map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	cfg := l.GetConfig(t)
	dmr, ok := cfg["dmr"].(map[string]any)
	require.True(t, ok)
	section, ok := dmr[string(k)].(map[string]any)
	if !ok {
		section = map[string]any{}
		dmr[string(k)] = section
	}
	for key, v := range change {
		section[key] = v
	}
	return Do(t, l.Router, l.Admin, http.MethodPut, "/api/v1/config", cfg)
}

// DMRSetting returns setting key of server k in the config that the
// config API serves.
func (l *LiveReplica) DMRSetting(t *testing.T, k manager.Kind, key string) any {
	t.Helper()
	dmr, ok := l.GetConfig(t)["dmr"].(map[string]any)
	require.True(t, ok)
	section, ok := dmr[string(k)].(map[string]any)
	if !ok {
		return nil
	}
	return section[key]
}

// First returns the first replica of the stack.
func (l *LiveStack) First() *LiveReplica {
	return l.first
}

// Addr returns the host:port of server k of the first replica.
func (l *LiveStack) Addr(t *testing.T, k manager.Kind) string {
	t.Helper()
	return l.first.Addr(t, k)
}

// GetConfig returns the config that the config API of the first replica
// serves.
func (l *LiveStack) GetConfig(t *testing.T) map[string]any {
	t.Helper()
	return l.first.GetConfig(t)
}

// SetDMR saves a change of server k with the config API of the first
// replica, as LiveReplica.SetDMR does.
func (l *LiveStack) SetDMR(t *testing.T, k manager.Kind, change map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return l.first.SetDMR(t, k, change)
}

// DMRSetting returns setting key of server k in the config that the
// config API of the first replica serves.
func (l *LiveStack) DMRSetting(t *testing.T, k manager.Kind, key string) any {
	t.Helper()
	return l.first.DMRSetting(t, k, key)
}
