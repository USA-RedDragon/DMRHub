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

package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func corsRouter(t *testing.T, enabled bool) *gin.Engine {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	cfg.HTTP.CORS.Enabled = enabled
	cfg.HTTP.CORS.Hosts = []string{"https://hub.example"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	ready := &atomic.Bool{}
	ready.Store(true)
	return internalhttp.CreateRouter(context.Background(), &cfg, nil, database, ps, ready, nil, "test", "test")
}

func corsRequest(t *testing.T, r *gin.Engine, method, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, path, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", origin)
	if method == http.MethodOptions {
		req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPublicLastheardAllowsAnyOriginWithoutCredentials(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		r := corsRouter(t, enabled)
		for _, origin := range []string{"https://radio.example", "https://hub.example"} {
			w := corsRequest(t, r, http.MethodGet, "/api/v1/lastheard", origin)
			assert.Equal(t, http.StatusOK, w.Code, "cors %v, origin %s", enabled, origin)
			assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"), "cors %v, origin %s", enabled, origin)
			assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"), "cors %v, origin %s", enabled, origin)

			w = corsRequest(t, r, http.MethodOptions, "/api/v1/lastheard", origin)
			assert.Equal(t, http.StatusNoContent, w.Code, "preflight, cors %v, origin %s", enabled, origin)
			assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"), "preflight, cors %v, origin %s", enabled, origin)
			assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), http.MethodGet, "preflight, cors %v, origin %s", enabled, origin)
			assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"), "preflight, cors %v, origin %s", enabled, origin)
		}
	}
}

func TestOtherRoutesKeepTheConfiguredCORS(t *testing.T) {
	t.Parallel()
	r := corsRouter(t, true)
	w := corsRequest(t, r, http.MethodGet, "/api/v1/lastheard/talkgroup/1", "https://radio.example")
	assert.NotEqual(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	w = corsRequest(t, r, http.MethodGet, "/api/v1/ping", "https://hub.example")
	assert.Equal(t, "https://hub.example", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}
