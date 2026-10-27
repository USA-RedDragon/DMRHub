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

package v1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

const testTimeout = 1 * time.Minute

func TestPingRoute(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/ping", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	// Convert ts (time.Now().Unix()) to int64
	var tsInt int64
	_, err = fmt.Sscanf(w.Body.String(), "%d", &tsInt)
	assert.NoError(t, err)

	w = httptest.NewRecorder()

	time.Sleep(1 * time.Second)

	ctx, cancel2 := context.WithTimeout(context.Background(), testTimeout)
	defer cancel2()

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/ping", nil)
	assert.NoError(t, err)

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var tsInt2 int64
	_, err = fmt.Sscanf(w.Body.String(), "%d", &tsInt2)
	assert.NoError(t, err)

	assert.Greater(t, tsInt2, tsInt)
}

func TestVersionRoute(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/version", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())
}

func TestNetworkFeaturesRoute(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		ipsc, openbridge, smtp   bool
		noRadioIDCheck           bool
		mmdvmPort, ipscPort      int
		openBridgePort           int
		public, canonical, wantS string
	}{
		{true, false, true, false, 62031, 50000, 62035, "", "https://hub.example.org", "hub.example.org"},
		{false, true, false, true, 62040, 50010, 62045, "dmr.example.org", "https://hub.example.org", "dmr.example.org"},
	} {
		router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
			c.DMR.MMDVM.Port = tc.mmdvmPort
			c.DMR.IPSC.Port = tc.ipscPort
			c.DMR.OpenBridge.Port = tc.openBridgePort
			c.DMR.IPSC.Enabled = tc.ipsc
			c.DMR.OpenBridge.Enabled = tc.openbridge
			c.SMTP.Enabled = tc.smtp
			c.DMR.DisableRadioIDValidation = tc.noRadioIDCheck
			c.DMR.PublicAddress = tc.public
			c.HTTP.CanonicalHost = tc.canonical
		})
		if err != nil {
			t.Fatalf("Failed to create test DB router: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/network/features", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		cancel()
		tdb.CloseDB()

		assert.Equal(t, http.StatusOK, w.Code)
		var got map[string]any
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, map[string]any{
			"ipsc": tc.ipsc, "openbridge": tc.openbridge, "password_reset": tc.smtp,
			"mmdvm_port": float64(tc.mmdvmPort), "ipsc_port": float64(tc.ipscPort),
			"openbridge_port":     float64(tc.openBridgePort),
			"server_address":      tc.wantS,
			"radio_id_validation": !tc.noRadioIDCheck,
			"registration":        "open",
			"visibility":          "public",
			"radio_id_checks": map[string]any{
				"user_id": !tc.noRadioIDCheck, "callsign": !tc.noRadioIDCheck, "radio_ids": !tc.noRadioIDCheck,
				"repeater_ids": !tc.noRadioIDCheck, "hotspot_ids": !tc.noRadioIDCheck,
			},
		}, got)
	}
}
