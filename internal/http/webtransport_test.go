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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/wtcert"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const maxInfoAllocs = 400

type webTransportInfo struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	Path    string `json:"path"`
	Hashes  []struct {
		Algorithm string `json:"algorithm"`
		Value     string `json:"value"`
	} `json:"certificate_hashes"`
}

func webTransportRouter(t *testing.T, enabled bool) (http.Handler, *config.Config) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:wt_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	cfg.Secret = "a secret"
	cfg.PasswordSalt = "a salt"
	cfg.DMR.MMDVM.WebTransport.Enabled = enabled
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
	return internalhttp.CreateRouter(context.Background(), &cfg, nil, database, ps, ready, nil, "test", "test"), &cfg
}

func askWebTransportInfo(t *testing.T, r http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/network/webtransport", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://radio.example")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func getWebTransportInfo(t *testing.T, enabled bool) (webTransportInfo, *httptest.ResponseRecorder, *config.Config) {
	t.Helper()
	r, cfg := webTransportRouter(t, enabled)
	w := askWebTransportInfo(t, r)
	var info webTransportInfo
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &info), w.Body.String())
	return info, w, cfg
}

// TestWebTransportInfoMakesNoCertificatePerRequest asks for the endpoint
// many times. The route is public, so it must reuse the certificates that
// it made, and not make and sign them again for each request.
func TestWebTransportInfoMakesNoCertificatePerRequest(t *testing.T) { //nolint:paralleltest // AllocsPerRun counts the allocations of every test that runs
	r, _ := webTransportRouter(t, true)
	require.Equal(t, http.StatusOK, askWebTransportInfo(t, r).Code)
	allocs := testing.AllocsPerRun(20, func() { askWebTransportInfo(t, r) })
	t.Logf("%.0f allocations a request", allocs)
	assert.Less(t, allocs, float64(maxInfoAllocs), "each request makes the certificates again")
}

func TestWebTransportInfoNamesTheListenerAndItsCertificates(t *testing.T) {
	t.Parallel()
	info, w, cfg := getWebTransportInfo(t, true)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"), "a radio app on any site reads it")
	assert.True(t, info.Enabled)
	assert.Equal(t, 62032, info.Port)
	assert.Equal(t, "/mmdvm", info.Path)

	certs, err := wtcert.New(cfg.GetDerivedSecret(), "", "")
	require.NoError(t, err)
	want, err := certs.Hashes()
	require.NoError(t, err)
	require.Len(t, info.Hashes, len(want))
	for i, h := range want {
		assert.Equal(t, "sha-256", info.Hashes[i].Algorithm)
		assert.Equal(t, base64.StdEncoding.EncodeToString(h[:]), info.Hashes[i].Value, "the hashes of the certificates that the listener serves")
	}
}

func TestWebTransportInfoWhenOff(t *testing.T) {
	t.Parallel()
	info, w, _ := getWebTransportInfo(t, false)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, info.Enabled)
	assert.Zero(t, info.Port)
	assert.Empty(t, info.Hashes)
}
