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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// LiveStack is an IntegrationStack whose DMR servers a manager.Manager
// runs, with an HTTP router whose config API applies a saved config to
// that manager, and an admin session.
type LiveStack struct {
	*IntegrationStack
	Manager *manager.Manager
	Router  *gin.Engine
	Admin   CookieJar
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
// IntegrationStack itself never start. PUT /api/v1/config saves the config
// file under the user config directory, so the caller points HOME and
// XDG_CONFIG_HOME at a temporary directory.
func SetupLiveStack(t *testing.T, backends ...Backend) *LiveStack {
	t.Helper()
	return setupLiveStack(t, false, backends)
}

// SetupRegisteredLiveStack makes a LiveStack, as SetupLiveStack does, and
// gives its hub a replica ID with Register before its servers start.
func SetupRegisteredLiveStack(t *testing.T, backends ...Backend) *LiveStack {
	t.Helper()
	return setupLiveStack(t, true, backends)
}

func setupLiveStack(t *testing.T, register bool, backends []Backend) *LiveStack {
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

	m := manager.New(manager.Deps{Hub: stack.Hub, DB: stack.DB, PubSub: stack.PubSub, KV: stack.KV, Version: "test", Commit: "test"})
	require.NoError(t, m.Start(context.Background(), cfg))
	t.Cleanup(func() { m.Stop(context.Background()) })

	ready := &atomic.Bool{}
	ready.Store(true)
	router := internalhttp.CreateRouter(context.Background(), cfg, stack.Hub, stack.DB, stack.PubSub, ready, nil, "test", "test", internalhttp.WithConfigApplier(m))

	hash, err := utils.HashPassword(TestPassword, cfg.PasswordSalt)
	require.NoError(t, err)
	require.NoError(t, stack.DB.Create(&models.User{ID: 9999901, Callsign: "N0ADM", Username: "liveadmin", Password: hash, Approved: true, Admin: true, SuperAdmin: true}).Error)

	return &LiveStack{IntegrationStack: stack, Manager: m, Router: router, Admin: Login(t, router, "liveadmin")}
}

// Addr returns the host:port of server k.
func (l *LiveStack) Addr(t *testing.T, k manager.Kind) string {
	t.Helper()
	a := l.Manager.Addr(k)
	require.NotNil(t, a, "the %s server is not running", k)
	return a.String()
}

// GetConfig returns the config that the config API serves.
func (l *LiveStack) GetConfig(t *testing.T) map[string]any {
	t.Helper()
	w := Do(t, l.Router, l.Admin, http.MethodGet, "/api/v1/config", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	return cfg
}

// SetDMR changes the settings of server k in the config that the config
// API serves and saves the config with the API.
func (l *LiveStack) SetDMR(t *testing.T, k manager.Kind, change map[string]any) *httptest.ResponseRecorder {
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
func (l *LiveStack) DMRSetting(t *testing.T, k manager.Kind, key string) any {
	t.Helper()
	dmr, ok := l.GetConfig(t)["dmr"].(map[string]any)
	require.True(t, ok)
	section, ok := dmr[string(k)].(map[string]any)
	if !ok {
		return nil
	}
	return section[key]
}
