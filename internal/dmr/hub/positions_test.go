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

package hub_test

import (
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func gpsPosition(t *testing.T, src uint, lat, lon float64, pe layer3.PositionAccuracy) dmrd.Position {
	t.Helper()
	var lc pdu.FullLinkControl
	require.NoError(t, lc.SetGPSInfo(pdu.FLCGPSInfo{Latitude: lat, Longitude: lon, PositionError: pe}))
	return dmrd.Position{Repeater: uint32(testRepeaterID), Source: uint32(src), Data: lc.RawPayload, Size: 18}
}

func storedPositions(t *testing.T, database *gorm.DB, radio uint) []models.Position {
	t.Helper()
	var out []models.Position
	require.NoError(t, database.Where("radio_id = ?", radio).Order("id").Find(&out).Error)
	return out
}

func TestEmbeddedGPSOfATrackedRadioIsStored(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedUser(t, database, testSrcID, "CALLER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	require.NoError(t, models.SetPositionTracking(database, testSrcID, testSrcID, true, time.Now().Add(-time.Minute)))

	h.RecordPosition(gpsPosition(t, testSrcID, 32.75, -97.25, layer3.PositionLessThan20M), models.RepeaterTypeModem, testRepeaterID)

	var got []models.Position
	require.Eventually(t, func() bool {
		got = storedPositions(t, database, testSrcID)
		return len(got) == 1
	}, 5*time.Second, 20*time.Millisecond, "the GPS of the RF call is a position of the tracked radio")
	p := got[0]
	assert.InDelta(t, 32.75, p.Latitude, 0.001)
	assert.InDelta(t, -97.25, p.Longitude, 0.001)
	assert.Equal(t, models.PositionFormat("gps"), p.Format)
	assert.Equal(t, testRepeaterID, p.RepeaterID)
	require.NotNil(t, p.AccuracyM)
	assert.InDelta(t, 20, *p.AccuracyM, 0)
}

func TestEmbeddedGPSFollowsTheTrackingRules(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	const untracked, deduped, unknown = testSrcID + 1, testSrcID + 2, testSrcID + 3
	seedUser(t, database, testSrcID, "CALLER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	require.NoError(t, models.SetPositionTracking(database, testSrcID, deduped, true, time.Now().Add(-time.Minute)))
	require.NoError(t, models.SetPositionTracking(database, testSrcID, unknown, true, time.Now().Add(-time.Minute)))
	lrrp, err := models.StorePosition(database, models.Position{RadioID: deduped, Latitude: 1, Longitude: 2, Format: models.PositionFormatLRRP, ReceivedAt: time.Now()})
	require.NoError(t, err)
	require.True(t, lrrp)

	h.RecordPosition(gpsPosition(t, untracked, 32.75, -97.25, layer3.PositionLessThan2M), models.RepeaterTypeMMDVM, testRepeaterID)
	h.RecordPosition(gpsPosition(t, deduped, 32.75, -97.25, layer3.PositionLessThan2M), models.RepeaterTypeMMDVM, testRepeaterID)
	h.RecordPosition(dmrd.Position{Repeater: uint32(testRepeaterID), Source: uint32(unknown), Size: 14}, models.RepeaterTypeMMDVM, testRepeaterID)
	h.RecordPosition(gpsPosition(t, unknown, 0, 0, layer3.PositionErrorUnknown), models.RepeaterTypeMMDVM, testRepeaterID)
	h.RecordPosition(gpsPosition(t, testSrcID, 10, 20, layer3.PositionLessThan2M), models.RepeaterTypeMMDVM, testRepeaterID)

	h.WaitBackground()
	require.NoError(t, models.SetPositionTracking(database, testSrcID, testSrcID, true, time.Now()))
	h.RecordPosition(gpsPosition(t, testSrcID, 30, 40, layer3.PositionErrorUnknown), models.RepeaterTypeMMDVM, testRepeaterID)
	h.RecordPosition(gpsPosition(t, testSrcID, 31, 41, layer3.PositionErrorUnknown), models.RepeaterTypeMMDVM, testRepeaterID)
	h.WaitBackground()

	own := storedPositions(t, database, testSrcID)
	require.Len(t, own, 1, "one position each 30 seconds, and none from before tracking started")
	assert.InDelta(t, 30, own[0].Latitude, 0.001)
	assert.Nil(t, own[0].AccuracyM, "an unknown position error has no accuracy")
	assert.Empty(t, storedPositions(t, database, untracked), "a radio with tracking off")
	assert.Len(t, storedPositions(t, database, deduped), 1, "the LRRP position of the radio came less than 30 seconds before")
	assert.Empty(t, storedPositions(t, database, unknown), "a short DMRG and a fix at 0, 0 have no position")
}

func TestStoredEmbeddedGPSIsPushed(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	const untracked = testSrcID + 1
	seedUser(t, database, testSrcID, "CALLER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	require.NoError(t, models.SetPositionTracking(database, testSrcID, testSrcID, true, time.Now().Add(-time.Minute)))
	var mu sync.Mutex
	var pushed []models.Position
	h.SetPositionNotify(func(p models.Position) {
		mu.Lock()
		defer mu.Unlock()
		pushed = append(pushed, p)
	})

	h.RecordPosition(gpsPosition(t, untracked, 32.75, -97.25, layer3.PositionLessThan2M), models.RepeaterTypeMMDVM, testRepeaterID)
	h.RecordPosition(gpsPosition(t, testSrcID, 32.75, -97.25, layer3.PositionLessThan2M), models.RepeaterTypeMMDVM, testRepeaterID)
	h.RecordPosition(gpsPosition(t, testSrcID, 32.76, -97.26, layer3.PositionLessThan2M), models.RepeaterTypeMMDVM, testRepeaterID)
	h.WaitBackground()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, pushed, 1, "only the stored position goes to the users and APRS-IS")
	assert.Equal(t, testSrcID, pushed[0].RadioID)
	assert.InDelta(t, 32.75, pushed[0].Latitude, 0.001)
}
