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

package config_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func section(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	s, ok := m[key].(map[string]any)
	require.True(t, ok, key)
	return s
}

func TestConfigSavesOpenBridgeSettings(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	router, jar := adminRouter(t)

	w := serve(t, router, http.MethodGet, "/api/v1/config", nil, jar)
	require.Equal(t, http.StatusOK, w.Code)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	dmr := section(t, cfg, "dmr")
	ob := section(t, dmr, "openbridge")
	assert.Equal(t, true, ob["enabled"])
	assert.Equal(t, "[::]", ob["bind"])
	assert.InDelta(t, 62035, ob["port"], 0)

	ob["server-id"] = 12
	body, err := json.Marshal(cfg)
	require.NoError(t, err)
	w = serve(t, router, http.MethodPost, "/api/v1/config/validate", body, jar)
	require.Equal(t, http.StatusOK, w.Code)
	var res struct {
		Valid  bool `json:"valid"`
		Errors struct {
			DMR struct {
				OpenBridge map[string]string `json:"openbridge"`
			} `json:"dmr"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.False(t, res.Valid)
	assert.Equal(t, map[string]string{"server-id": "the OpenBridge server ID must be 0 or 4 to 7 digits"}, res.Errors.DMR.OpenBridge)

	cfg["secret"] = "a-secret-that-is-long-enough-for-the-check"
	cfg["password-salt"] = "a-salt-that-is-long-enough-for-the-check"
	section(t, cfg, "database")["database"] = "DMRHub.db"
	section(t, cfg, "http")["canonical-host"] = "http://localhost"
	section(t, dmr, "ipsc")["network-id"] = 311860
	ob["server-id"] = 3129
	ob["bind"] = "127.0.0.1"
	ob["port"] = 62040
	body, err = json.Marshal(cfg)
	require.NoError(t, err)
	w = serve(t, router, http.MethodPut, "/api/v1/config", body, jar)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = serve(t, router, http.MethodGet, "/api/v1/config", nil, jar)
	require.Equal(t, http.StatusOK, w.Code)
	var got struct {
		DMR struct {
			OpenBridge map[string]any `json:"openbridge"`
		} `json:"dmr"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, map[string]any{"enabled": true, "bind": "127.0.0.1", "port": 62040.0, "server-id": 3129.0}, got.DMR.OpenBridge)

	saved, err := os.ReadFile(filepath.Join(configHome, "DMRHub", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(saved), "server-id: 3129")
}
