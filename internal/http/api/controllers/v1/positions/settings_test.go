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

package positions_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (e *env) put(path string, body any, out any) int {
	e.t.Helper()
	w := testutils.Do(e.t, e.router, e.jar, http.MethodPut, path, body)
	if out != nil && w.Code == http.StatusOK {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), out), w.Body.String())
	}
	return w.Code
}

func settings(tracking bool, interval, retention uint) map[string]any {
	return map[string]any{"tracking": tracking, "interval_minutes": interval, "retention_hours": retention}
}

func withAPRS(cfg *config.Config) {
	cfg.DMR.Positions.MinInterval = 15
	cfg.DMR.Positions.MaxRetention = 90
	cfg.DMR.Positions.APRSIS.Callsign = "N0HUB-10"
}

func TestPositionRadiosListTheUsersRadioIDsAndTheHubsChoices(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	added := uint(3191869)
	require.NoError(t, e.db.Create(&models.UserRadioID{UserID: owner, RadioID: added}).Error)
	var r apimodels.PositionRadios
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/positions/radios", &r))
	require.Len(t, r.Radios, 2)
	assert.Equal(t, owner, r.Radios[0].RadioID)
	assert.Equal(t, added, r.Radios[1].RadioID)
	assert.False(t, r.Radios[0].Tracking)
	assert.Equal(t, uint(10), r.Radios[0].IntervalMinutes)
	assert.Equal(t, uint(24), r.Radios[0].RetentionHours)
	assert.Equal(t, []uint{5, 10, 15, 30, 60}, r.Intervals)
	assert.Equal(t, []uint{24, 168, 720}, r.Retention)
	assert.False(t, r.APRS)
	assert.Equal(t, "KI5VMF", r.Callsign)

	e = newEnv(t, withAPRS)
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/positions/radios", &r))
	assert.Equal(t, []uint{15, 30, 60}, r.Intervals)
	assert.Equal(t, []uint{24, 168, 720, 2160}, r.Retention)
	assert.True(t, r.APRS)
}

func TestOnlyTheOwnerSetsARadiosPositionSetting(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	assert.Equal(t, http.StatusNotFound, e.put(fmt.Sprintf("/api/v1/positions/%d/settings", other), settings(true, 10, 24), nil))
	assert.Equal(t, http.StatusNotFound, e.put("/api/v1/positions/9999999/settings", settings(true, 10, 24), nil))
	_, ok, err := models.FindPositionSetting(e.db, other)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestPositionSettingsOutsideTheHubsChoicesAreRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t, withAPRS)
	path := fmt.Sprintf("/api/v1/positions/%d/settings", owner)
	aprsOn := func(m map[string]any) map[string]any { m["aprs"] = true; return m }
	for name, body := range map[string]map[string]any{
		"below the floor":    settings(true, 10, 24),
		"not a choice":       settings(true, 20, 24),
		"retention":          settings(true, 15, 48),
		"over the max":       settings(true, 15, 2400),
		"aprs without track": aprsOn(settings(false, 15, 24)),
		"no tracking":        {"interval_minutes": 15, "retention_hours": 24},
		"ssid":               func() map[string]any { m := settings(true, 15, 24); m["aprs_ssid"] = "16"; return m }(),
		"symbol":             func() map[string]any { m := settings(true, 15, 24); m["aprs_symbol"] = "ufo"; return m }(),
		"comment":            func() map[string]any { m := settings(true, 15, 24); m["aprs_comment"] = "a|b"; return m }(),
	} {
		assert.Equal(t, http.StatusBadRequest, e.put(path, body, nil), name)
	}
	e = newEnv(t)
	assert.Equal(t, http.StatusBadRequest, e.put(path, aprsOn(settings(true, 10, 24)), nil), "the hub has no APRS-IS login")
}

func TestThePositionSettingIsStored(t *testing.T) {
	t.Parallel()
	e := newEnv(t, withAPRS)
	body := settings(true, 30, 168)
	body["aprs"], body["aprs_ssid"], body["aprs_symbol"], body["aprs_comment"] = true, "7", "person", "DMR 1000001"
	var got apimodels.PositionRadio
	require.Equal(t, http.StatusOK, e.put(fmt.Sprintf("/api/v1/positions/%d/settings", owner), body, &got))
	assert.Equal(t, apimodels.PositionRadio{RadioID: owner, Tracking: true, IntervalMinutes: 30, RetentionHours: 168,
		APRS: true, APRSSSID: "7", APRSSymbol: "person", APRSComment: "DMR 1000001"}, got)
	s, ok, err := models.FindPositionSetting(e.db, owner)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, s.Tracking)
	assert.Equal(t, owner, s.UserID)
}

func (e *env) openRequest(interval uint) {
	e.t.Helper()
	require.NoError(e.t, e.db.Create(&models.LocationRequest{RadioID: owner, ServiceID: 5057, RequestID: []byte{0, 0, 0, 1},
		IntervalSeconds: interval, SentAt: time.Now(), RepeaterID: 319186801, Slot: 2}).Error)
}

func TestTurningTrackingOffStopsTheRequestAndDeletesThePositions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.track(owner, time.Hour)
	e.openRequest(600)
	require.Equal(t, http.StatusOK, e.put(fmt.Sprintf("/api/v1/positions/%d/settings", owner), settings(false, 10, 24), nil))
	var n int64
	require.NoError(t, e.db.Model(&models.LocationRequest{}).Count(&n).Error)
	assert.Zero(t, n, "the stop request forgets the open request")
	require.NoError(t, e.db.Model(&models.Position{}).Count(&n).Error)
	assert.Zero(t, n)
}

func TestANewIntervalReplacesTheOpenRequest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.track(owner)
	e.openRequest(600)
	require.Equal(t, http.StatusOK, e.put(fmt.Sprintf("/api/v1/positions/%d/settings", owner), settings(true, 30, 24), nil))
	var r models.LocationRequest
	require.NoError(t, e.db.Where("radio_id = ?", owner).Take(&r).Error)
	assert.Equal(t, uint(1800), r.IntervalSeconds)
	assert.NotEqual(t, []byte{0, 0, 0, 1}, r.RequestID)
}

func TestTheOwnersTrackingChangesReachThePlugins(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(cfg *config.Config) { cfg.DMR.Positions.MaxRetention = 90 })
	sub := e.ps.Subscribe(plugins.TrackingTopic)
	t.Cleanup(func() { _ = sub.Close() })
	path := fmt.Sprintf("/api/v1/positions/%d/settings", owner)
	told := func(what string) {
		t.Helper()
		select {
		case b := <-sub.Channel():
			assert.Equal(t, strconv.FormatUint(uint64(owner), 10), string(b), what)
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: the plugins were not told", what)
		}
	}
	require.Equal(t, http.StatusOK, e.put(path, settings(true, 10, 24), nil))
	told("tracking on")
	require.Equal(t, http.StatusOK, e.put(path, settings(true, 30, 24), nil))
	told("a new interval")
	require.Equal(t, http.StatusOK, e.put(path, settings(true, 30, 168), nil))
	select {
	case <-sub.Channel():
		t.Fatal("a new retention is no news to the plugins")
	case <-time.After(200 * time.Millisecond):
	}
	require.Equal(t, http.StatusOK, e.put(path, settings(false, 30, 168), nil))
	told("tracking off")
}

func TestSharingPositionsIsAUserSetting(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var s apimodels.UserSettings
	w := testutils.Do(t, e.router, e.jar, http.MethodPut, "/api/v1/users/me/settings/share-positions", map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))
	assert.True(t, s.SharePositions)
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/users/me/settings", &s))
	assert.True(t, s.SharePositions)
	assert.Equal(t, http.StatusBadRequest, testutils.Do(t, e.router, e.jar, http.MethodPut, "/api/v1/users/me/settings/share-positions", map[string]any{}).Code)
}
