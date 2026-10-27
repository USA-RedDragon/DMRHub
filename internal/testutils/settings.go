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
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// SetupReady fills in the settings that a save of the Setup settings
// needs, so that SaveSetting works on a router made with c. Accounts that
// SeedAccount makes cannot log in on such a router, since it changes the
// password salt.
func SetupReady(c *config.Config) {
	c.HTTP.CanonicalHost = "http://localhost:3005"
	c.Secret = "test-secret-of-sixteen"
	c.PasswordSalt = "test-salt-of-sixteen"
	c.DMR.IPSC.NetworkID = 1
}

// SaveSetting sets the Setup setting at the dotted path, such as
// http.session-length, to value through the config API as the admin of
// jar, as the Setup page does. The router must be made with a config that
// SetupReady changed.
func SaveSetting(t *testing.T, router *gin.Engine, jar CookieJar, path string, value any) {
	t.Helper()
	w := Do(t, router, jar, http.MethodGet, "/api/v1/config", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	parts := strings.Split(path, ".")
	node := cfg
	for _, p := range parts[:len(parts)-1] {
		next, ok := node[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			node[p] = next
		}
		node = next
	}
	node[parts[len(parts)-1]] = value
	w = Do(t, router, jar, http.MethodPut, "/api/v1/config", cfg)
	require.Equal(t, http.StatusOK, w.Code, "saving %s: %s", path, w.Body.String())
}
