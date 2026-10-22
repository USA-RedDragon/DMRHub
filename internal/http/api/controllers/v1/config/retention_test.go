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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func adminRequest(t *testing.T, router *gin.Engine, jar testutils.CookieJar, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	require.NoError(t, err)
	for _, c := range jar.Cookies() {
		req.Header.Add("Cookie", c.String())
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestConfigCarriesRetention(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	_, _, jar := testutils.LoginUser(t, router, apimodels.AuthLogin{Username: "Admin", Password: "password"})

	w := adminRequest(t, router, jar, http.MethodGet, "/api/v1/config", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	dmr, ok := cfg["dmr"].(map[string]any)
	require.True(t, ok)
	retention, ok := dmr["retention"].(map[string]any)
	require.True(t, ok, "dmr.retention is in the config")
	assert.Equal(t, map[string]any{"messages": 0.0, "transmissions": 0.0, "bursts": 0.0}, retention)

	retention["messages"] = -5
	retention["bursts"] = 36501
	body, err := json.Marshal(cfg)
	require.NoError(t, err)
	w = adminRequest(t, router, jar, http.MethodPost, "/api/v1/config/validate", body)
	require.Equal(t, http.StatusOK, w.Code)
	var res struct {
		Valid  bool `json:"valid"`
		Errors struct {
			DMR struct {
				Retention map[string]string `json:"retention"`
			} `json:"dmr"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.False(t, res.Valid)
	assert.Contains(t, res.Errors.DMR.Retention, "messages")
	assert.Contains(t, res.Errors.DMR.Retention, "bursts")
	assert.NotContains(t, res.Errors.DMR.Retention, "transmissions")
}
