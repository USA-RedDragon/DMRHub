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

package models_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedPositionUsers(t *testing.T, database *gorm.DB) {
	t.Helper()
	require.NoError(t, database.Create(&models.User{ID: 3191868, Callsign: "KI5VMF", Username: "a", Approved: true}).Error)
	require.NoError(t, database.Create(&models.User{ID: 3140598, Callsign: "KD5SMP", Username: "b", Approved: true}).Error)
	require.NoError(t, database.Create(&models.User{ID: 3100001, Callsign: "N0CALL", Username: "c", Approved: true}).Error)
}

func fixAt(radio uint, at time.Time) models.Position {
	return models.Position{RadioID: radio, Latitude: 30.2672, Longitude: -97.7431, Format: models.PositionFormatLRRP, ReceivedAt: at, RepeaterID: 319186801, Slot: 2, ServiceID: 5057}
}

func TestPositionsAreStoredOnlyForTrackedRadiosFromWhenTrackingStarted(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	seedPositionUsers(t, database)
	now := time.Now()

	stored, err := models.StorePosition(database, fixAt(3191868, now))
	require.NoError(t, err)
	assert.False(t, stored, "a radio whose owner never turned tracking on")

	require.NoError(t, models.SetPositionTracking(database, 3191868, 3191868, true, now))
	stored, err = models.StorePosition(database, fixAt(3191868, now.Add(-time.Minute)))
	require.NoError(t, err)
	assert.False(t, stored, "a report from before tracking started")
	stored, err = models.StorePosition(database, fixAt(3191868, now.Add(time.Second)))
	require.NoError(t, err)
	assert.True(t, stored)
}

func TestAtMostOnePositionPerRadioEach30Seconds(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	seedPositionUsers(t, database)
	now := time.Now()
	require.NoError(t, models.SetPositionTracking(database, 3191868, 3191868, true, now))

	for i, want := range []bool{true, false, false, true} {
		at := now.Add([]time.Duration{time.Second, 10 * time.Second, 30 * time.Second, 32 * time.Second}[i])
		stored, err := models.StorePosition(database, fixAt(3191868, at))
		require.NoError(t, err)
		assert.Equal(t, want, stored, "report %d", i)
	}
}

func TestTurningTrackingOffDeletesThePositions(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	seedPositionUsers(t, database)
	now := time.Now()
	require.NoError(t, models.SetPositionTracking(database, 3191868, 3191868, true, now))
	_, err := models.StorePosition(database, fixAt(3191868, now.Add(time.Second)))
	require.NoError(t, err)

	require.NoError(t, models.SetPositionTracking(database, 3191868, 3191868, false, now.Add(time.Minute)))
	var n int64
	require.NoError(t, database.Model(&models.Position{}).Where("radio_id = ?", 3191868).Count(&n).Error)
	assert.Zero(t, n)
	stored, err := models.StorePosition(database, fixAt(3191868, now.Add(2*time.Minute)))
	require.NoError(t, err)
	assert.False(t, stored)
}

func TestPrunePositionsKeepsEachRadiosRetention(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	seedPositionUsers(t, database)
	start := time.Now().Add(-10 * 24 * time.Hour)
	for _, radio := range []uint{3191868, 3140598} {
		require.NoError(t, models.SetPositionTracking(database, radio, radio, true, start))
	}
	require.NoError(t, models.SetPositionRetention(database, 3140598, 7*24*time.Hour))
	for _, radio := range []uint{3191868, 3140598} {
		for _, age := range []time.Duration{9 * 24 * time.Hour, 3 * 24 * time.Hour, time.Hour} {
			_, err := models.StorePosition(database, fixAt(radio, time.Now().Add(-age)))
			require.NoError(t, err)
		}
	}

	require.NoError(t, models.PrunePositions(database, time.Now()))
	count := func(radio uint) int64 {
		var n int64
		require.NoError(t, database.Model(&models.Position{}).Where("radio_id = ?", radio).Count(&n).Error)
		return n
	}
	assert.Equal(t, int64(1), count(3191868), "24 hours, the default")
	assert.Equal(t, int64(2), count(3140598), "7 days")
}

func TestLatestPositionsFollowSharing(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	seedPositionUsers(t, database)
	now := time.Now()
	for _, radio := range []uint{3191868, 3140598, 3100001} {
		require.NoError(t, models.SetPositionTracking(database, radio, radio, true, now))
		_, err := models.StorePosition(database, fixAt(radio, now.Add(time.Second)))
		require.NoError(t, err)
	}
	ids := func(viewer uint) []uint {
		got, err := models.LatestPositions(database, viewer, now.Add(-24*time.Hour))
		require.NoError(t, err)
		out := make([]uint, 0, len(got))
		for _, p := range got {
			out = append(out, p.RadioID)
		}
		return out
	}
	assert.Equal(t, []uint{3191868}, ids(3191868), "private by default")

	require.NoError(t, models.SetSharePositions(database, 3140598, true))
	assert.Equal(t, []uint{3191868}, ids(3191868), "a user who does not share sees no shared radios")
	require.NoError(t, models.SetSharePositions(database, 3191868, true))
	assert.ElementsMatch(t, []uint{3191868, 3140598}, ids(3191868))
	assert.ElementsMatch(t, []uint{3140598, 3191868}, ids(3140598))
	assert.Equal(t, []uint{3100001}, ids(3100001))
}
