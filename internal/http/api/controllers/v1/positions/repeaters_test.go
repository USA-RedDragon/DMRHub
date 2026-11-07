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
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ownRepeater      = uint(310999901)
	ownSilent        = uint(310999902)
	ownGone          = uint(310999903)
	otherRepeater    = uint(310999801)
	cosysopRepeater  = uint(310999802)
	ownClosed        = uint(310999904)
	repeaterLat      = 30.2669
	repeaterLon      = -97.7729
	repeaterLocation = "Zilker Park"
)

func (e *env) repeater(id, owner uint, lat, lon float64, lastPing time.Duration) {
	e.t.Helper()
	now := time.Now()
	r := models.Repeater{Type: models.RepeaterTypeMMDVM, OwnerID: owner, Connected: now.Add(-time.Hour), LastPing: now.Add(-lastPing),
		RepeaterConfiguration: models.RepeaterConfiguration{ID: id, Callsign: "N0CALL", Latitude: lat, Longitude: lon, Height: 12,
			Location: repeaterLocation, Description: "Test repeater"}}
	require.NoError(e.t, e.db.Create(&r).Error)
}

func (e *env) repeaterIDs(jar testutils.CookieJar) []uint {
	e.t.Helper()
	var l apimodels.RepeaterPositionList
	require.Equal(e.t, http.StatusOK, e.get(jar, "/api/v1/positions/repeaters", &l))
	ids := make([]uint, 0, len(l.Repeaters))
	for _, r := range l.Repeaters {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestRepeaterPositionsShowConnectedRepeatersThatReportAPosition(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.repeater(ownRepeater, owner, repeaterLat, repeaterLon, 5*time.Second)
	e.repeater(ownSilent, owner, 0, 0, 5*time.Second)
	e.repeater(ownGone, owner, repeaterLat, repeaterLon, 2*models.RepeaterOnline)
	e.repeater(otherRepeater, other, repeaterLat, repeaterLon, 5*time.Second)
	e.repeater(ownClosed, owner, repeaterLat, repeaterLon, 5*time.Second)
	require.NoError(t, models.SetRepeaterDisconnected(e.db, ownClosed, time.Now()))

	var l apimodels.RepeaterPositionList
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/positions/repeaters", &l))
	require.Len(t, l.Repeaters, 1)
	r := l.Repeaters[0]
	assert.Equal(t, ownRepeater, r.ID)
	assert.Equal(t, "N0CALL", r.Callsign)
	assert.InDelta(t, repeaterLat, r.Latitude, 1e-9)
	assert.InDelta(t, repeaterLon, r.Longitude, 1e-9)
	assert.Equal(t, uint16(12), r.Height)
	assert.Equal(t, repeaterLocation, r.Location)
	assert.False(t, r.Connected.IsZero())
	assert.False(t, r.LastPing.IsZero())
}

func TestRepeaterPositionsFollowTheRepeatersPageVisibility(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.repeater(ownRepeater, owner, repeaterLat, repeaterLon, 5*time.Second)
	e.repeater(otherRepeater, other, repeaterLat, repeaterLon, 5*time.Second)
	e.repeater(cosysopRepeater, other, repeaterLat, repeaterLon, 5*time.Second)
	now := time.Now()
	require.NoError(t, e.db.Create(&models.RepeaterCosysop{RepeaterID: cosysopRepeater, UserID: owner, InvitedBy: other,
		InvitedAt: now, ExpiresAt: now.Add(time.Hour), AcceptedAt: &now}).Error)

	assert.Equal(t, []uint{cosysopRepeater, ownRepeater}, e.repeaterIDs(e.jar), "own and co-sysop")
	assert.Equal(t, []uint{otherRepeater, cosysopRepeater}, e.repeaterIDs(testutils.Login(t, e.router, "other")))
	assert.Empty(t, e.repeaterIDs(testutils.Login(t, e.router, "third")))

	require.NoError(t, e.db.Model(&models.User{}).Where("id = ?", third).Update("admin", true).Error)
	assert.Equal(t, []uint{otherRepeater, cosysopRepeater, ownRepeater}, e.repeaterIDs(testutils.Login(t, e.router, "third")), "an admin sees all")
}

func TestRepeaterPositionsNeedALogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	assert.Equal(t, http.StatusUnauthorized, e.get(testutils.CookieJar{}, "/api/v1/positions/repeaters", nil))
}

func TestRepeaterPositionsListTheTalkgroupsEachRepeaterCarries(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.repeater(ownRepeater, owner, repeaterLat, repeaterLon, 5*time.Second)
	for _, id := range []uint{3100, 3101, 3102} {
		require.NoError(t, e.db.Create(&models.Talkgroup{ID: id, Name: "Test"}).Error)
	}
	var r models.Repeater
	require.NoError(t, e.db.First(&r, ownRepeater).Error)
	require.NoError(t, e.db.Model(&r).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 3101}))
	require.NoError(t, e.db.Model(&r).Association("TS2StaticTalkgroups").Append(&models.Talkgroup{ID: 3100}, &models.Talkgroup{ID: 3101}))
	tg := uint(3102)
	require.NoError(t, e.db.Model(&models.Repeater{}).Where("id = ?", ownRepeater).Update("ts1_dynamic_talkgroup_id", tg).Error)

	var l apimodels.RepeaterPositionList
	require.Equal(t, http.StatusOK, e.get(e.jar, "/api/v1/positions/repeaters", &l))
	require.Len(t, l.Repeaters, 1)
	assert.Equal(t, []uint{3100, 3101, 3102}, l.Repeaters[0].Talkgroups)
}
