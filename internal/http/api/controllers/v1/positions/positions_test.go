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
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	owner = uint(1000001)
	other = uint(1000002)
	third = uint(1000003)
)

type env struct {
	t      *testing.T
	router *gin.Engine
	db     *gorm.DB
	ps     pubsub.PubSub
	jar    testutils.CookieJar
}

func newEnv(t *testing.T, opts ...testutils.ConfigOption) *env {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(opts...)
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	db := tdb.DB()
	testutils.SeedAccount(t, db, owner, "KI5VMF", "owner")
	testutils.SeedAccount(t, db, other, "KD5SMP", "other")
	testutils.SeedAccount(t, db, third, "N0CALL", "third")
	return &env{t: t, router: router, db: db, ps: tdb.PubSub(), jar: testutils.Login(t, router, "owner")}
}

func (e *env) track(radio uint, ages ...time.Duration) {
	e.t.Helper()
	require.NoError(e.t, models.SetPositionTracking(e.db, radio, radio, true, time.Now().Add(-40*24*time.Hour)))
	for i, age := range ages {
		acc := 10.0
		p := models.Position{RadioID: radio, Latitude: 30.25 + float64(i)/100, Longitude: -97.75, Format: models.PositionFormatLRRP,
			AccuracyM: &acc, ReceivedAt: time.Now().Add(-age), RepeaterID: 319186801, Slot: 2, ServiceID: 5057}
		require.NoError(e.t, e.db.Create(&p).Error)
	}
}

func (e *env) get(jar testutils.CookieJar, path string, out any) int {
	e.t.Helper()
	w := testutils.Do(e.t, e.router, jar, http.MethodGet, path, nil)
	if out != nil && w.Code == http.StatusOK {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), out), w.Body.String())
	}
	return w.Code
}

func TestPositionsListsTheViewersRadiosOfTheLastDay(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.track(owner, 2*time.Hour, time.Hour)
	e.track(other, time.Hour)
	stale := uint(1000004)
	testutils.SeedAccount(t, e.db, stale, "N1OLD", "stale")
	e.track(stale, 30*time.Hour)

	var l apimodels.PositionList
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/positions", &l))
	require.Len(t, l.Positions, 1)
	p := l.Positions[0]
	assert.Equal(t, owner, p.RadioID)
	assert.InDelta(t, 30.26, p.Latitude, 1e-9, "the newest")
	assert.Equal(t, "lrrp", p.Format)
	require.NotNil(t, p.AccuracyM)
	assert.Nil(t, p.SpeedMPS)
	assert.Equal(t, uint(319186801), p.RepeaterID)
	assert.Equal(t, uint8(2), p.Slot)
}

func TestPositionsOfAnotherUsersRadioAreHiddenUnlessItsOwnerShares(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.track(other, time.Hour)
	assert.Equal(t, http.StatusNotFound, e.get(e.jar, fmt.Sprintf("/api/v1/positions/%d", other), nil))

	require.NoError(t, models.SetSharePositions(e.db, other, true))
	var r apimodels.RadioPositions
	require.Equal(t, http.StatusOK, e.get(e.jar, fmt.Sprintf("/api/v1/positions/%d", other), &r))
	require.NotNil(t, r.Latest)
	assert.Len(t, r.Track, 1)

	var l apimodels.PositionList
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/positions", &l))
	require.Len(t, l.Positions, 1)
	assert.Equal(t, other, l.Positions[0].RadioID)
	assert.True(t, l.Positions[0].Shared)
	assert.Equal(t, "KD5SMP", l.Positions[0].Callsign)
}

func TestARadiosTrackIsTheLastDayOldestFirst(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.track(owner, 30*time.Hour, 3*time.Hour, time.Hour)
	var r apimodels.RadioPositions
	require.Equal(t, http.StatusOK, e.get(e.jar, fmt.Sprintf("/api/v1/positions/%d", owner), &r))
	require.Len(t, r.Track, 2)
	assert.True(t, r.Track[0].ReceivedAt.Before(r.Track[1].ReceivedAt))
	assert.Equal(t, r.Track[1].Latitude, r.Latest.Latitude)
}

func TestOnlyTheOwnerExportsATrack(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.track(owner, 3*time.Hour, time.Hour)
	e.track(other, time.Hour)
	require.NoError(t, models.SetSharePositions(e.db, owner, true))
	require.NoError(t, models.SetSharePositions(e.db, other, true))

	w := testutils.Do(t, e.router, e.jar, http.MethodGet, fmt.Sprintf("/api/v1/positions/%d/track?format=csv", other), nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "a shared radio is not the viewer's to export")

	w = testutils.Do(t, e.router, e.jar, http.MethodGet, fmt.Sprintf("/api/v1/positions/%d/track?format=csv", owner), nil)
	require.Equal(t, http.StatusOK, w.Code)
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "received_at", rows[0][0])

	w = testutils.Do(t, e.router, e.jar, http.MethodGet, fmt.Sprintf("/api/v1/positions/%d/track?format=gpx", owner), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var gpx struct {
		Points []struct {
			Lat float64 `xml:"lat,attr"`
			Lon float64 `xml:"lon,attr"`
		} `xml:"trk>trkseg>trkpt"`
	}
	require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &gpx))
	assert.Len(t, gpx.Points, 2)

	w = testutils.Do(t, e.router, e.jar, http.MethodGet, fmt.Sprintf("/api/v1/positions/%d/track?format=geojson", owner), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var gj struct {
		Type     string `json:"type"`
		Features []struct {
			Geometry struct {
				Type        string      `json:"type"`
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &gj))
	require.Len(t, gj.Features, 1)
	assert.Equal(t, "LineString", gj.Features[0].Geometry.Type)
	require.Len(t, gj.Features[0].Geometry.Coordinates, 2)
	assert.InDelta(t, -97.75, gj.Features[0].Geometry.Coordinates[0][0], 1e-9, "GeoJSON puts the longitude first")

	w = testutils.Do(t, e.router, e.jar, http.MethodGet, fmt.Sprintf("/api/v1/positions/%d/track?format=kml", owner), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUsersMeReportsPositionTracking(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var me struct {
		PositionTracking bool `json:"position_tracking"`
	}
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/users/me", &me))
	assert.False(t, me.PositionTracking)
	e.track(owner)
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/users/me", &me))
	assert.True(t, me.PositionTracking)
}

func TestPositionsNeedALogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	assert.Equal(t, http.StatusUnauthorized, e.get(testutils.CookieJar{}, "/api/v1/positions", nil))
}

func TestTheWindowOfThePositionsIsADayAWeekOrAMonth(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.track(owner, 20*24*time.Hour, 3*24*time.Hour, time.Hour)
	var l apimodels.PositionList
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/positions?hours=720", &l))
	require.Len(t, l.Positions, 1)
	var r apimodels.RadioPositions
	require.Equal(t, http.StatusOK, e.get(e.jar, fmt.Sprintf("/api/v1/positions/%d?hours=168", owner), &r))
	assert.Len(t, r.Track, 2)
	require.Equal(t, http.StatusOK, e.get(e.jar, fmt.Sprintf("/api/v1/positions/%d?hours=720", owner), &r))
	assert.Len(t, r.Track, 3)
	assert.Equal(t, uint(5057), r.Track[0].ServiceID)
	require.Equal(t, http.StatusOK, e.get(e.jar, fmt.Sprintf("/api/v1/positions/%d", owner), &r))
	assert.Len(t, r.Track, 1, "a day by default")

	e.track(other, 2*24*time.Hour)
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/positions?hours=168", &l))
	assert.Len(t, l.Positions, 1, "still only the radios the user sees")
	for _, bad := range []string{"48", "0", "-1", "x", "8760"} {
		assert.Equal(t, http.StatusBadRequest, e.get(e.jar, "/api/v1/positions?hours="+bad, nil), bad)
		assert.Equal(t, http.StatusBadRequest, e.get(e.jar, fmt.Sprintf("/api/v1/positions/%d?hours=%s", owner, bad), nil), bad)
	}
}
