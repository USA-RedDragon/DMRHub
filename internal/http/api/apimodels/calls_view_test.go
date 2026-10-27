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

package apimodels_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type allSMS struct{}

func (w allSMS) VisibleSMS(db *gorm.DB, _ *models.User) *gorm.DB {
	return db.Model(&models.Call{}).Select("id")
}

func (w allSMS) Visible(db *gorm.DB, viewer *models.User, ids []uint) (map[uint]bool, error) {
	visible, err := models.CallIDsIn(db, ids, w.VisibleSMS(db, viewer))
	if err != nil {
		return nil, fmt.Errorf("finding visible calls: %w", err)
	}
	return visible, nil
}

func smsCall() models.Call {
	sap := uint8(4)
	return models.Call{
		ID: 7, Kind: models.CallKindData, SubKind: models.SubKindSMS, SourceID: 3140598, DestinationID: 3191868,
		SAP: &sap, DataFormat: "ip", Confirmed: true, Blocks: 3, Octets: 44, Bursts: 4, Response: models.CallResponseACK,
		Duration: 600 * time.Millisecond,
	}
}

func TestMaskSMSShowsPlainData(t *testing.T) {
	t.Parallel()
	c := smsCall()
	resp := apimodels.NewWSCallResponseFromCall(&c)
	resp.MaskSMS()
	assert.Equal(t, models.CallKindData, resp.Kind)
	assert.Empty(t, resp.SubKind)
	assert.Nil(t, resp.SAP)
	assert.Empty(t, resp.DataFormat)
	assert.False(t, resp.Confirmed)
	assert.Zero(t, resp.Blocks)
	assert.Zero(t, resp.Octets)
	assert.Zero(t, resp.Bursts)
	assert.Equal(t, models.CallResponseNone, resp.Response)
	assert.Equal(t, 600*time.Millisecond, resp.Duration)

	other := models.Call{Kind: models.CallKindData, SubKind: "lrrp", Octets: 24}
	lrrp := apimodels.NewWSCallResponseFromCall(&other)
	lrrp.MaskSMS()
	assert.Equal(t, "lrrp", lrrp.SubKind, "only SMS rows are masked")
	assert.Equal(t, uint(24), lrrp.Octets)
}

func TestMaidenhead(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "EM12", apimodels.Maidenhead(32.7767, -96.797))
	assert.Equal(t, "JN58", apimodels.Maidenhead(48.1, 11.6))
	assert.Equal(t, "RR99", apimodels.Maidenhead(90, 180))
	assert.Empty(t, apimodels.Maidenhead(91, 0))
}

func TestResponsesForViewers(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	msg := smsCall()
	msg.ID = 0
	require.NoError(t, database.Create(&msg).Error)
	lat, lon := 32.7767, -96.797
	gps := models.Call{Kind: models.CallKindGPS, SourceID: 3191868, Latitude: &lat, Longitude: &lon, GroupCall: true, DestinationID: 1}
	require.NoError(t, database.Create(&gps).Error)
	calls := []models.Call{msg, gps}

	owner := &models.User{ID: 3191868, Approved: true}
	stranger := &models.User{ID: 3113042, Approved: true}
	admin := &models.User{ID: 1, Approved: true, Admin: true}

	for _, tc := range []struct {
		name     string
		viewer   *models.User
		window   models.SMSWindow
		wantSMS  bool
		wantSpot bool
	}{
		{"visitor", nil, allSMS{}, false, false},
		{"masked window", owner, models.MaskedSMSWindow{}, false, true},
		{"inside window", owner, allSMS{}, true, true},
		{"stranger", stranger, models.MaskedSMSWindow{}, false, false},
		{"admin", admin, models.MaskedSMSWindow{}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows, err := apimodels.NewWSCallResponsesFor(database, tc.viewer, tc.window, calls)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			if tc.wantSMS {
				assert.Equal(t, models.SubKindSMS, rows[0].SubKind)
				assert.Equal(t, uint(44), rows[0].Octets)
			} else {
				assert.Empty(t, rows[0].SubKind)
				assert.Zero(t, rows[0].Octets)
			}
			assert.Equal(t, "EM12", rows[1].Grid)
			if tc.wantSpot {
				assert.NotNil(t, rows[1].Latitude)
			} else {
				assert.Nil(t, rows[1].Latitude)
				assert.Nil(t, rows[1].Longitude)
			}
			out, err := json.Marshal(rows)
			require.NoError(t, err)
			var keys []map[string]any
			require.NoError(t, json.Unmarshal(out, &keys))
			for _, k := range []string{"email", "text", "raw", "call_data"} {
				assert.NotContains(t, keys[0], k)
			}
		})
	}
}

func TestResponseOfUnregisteredSource(t *testing.T) {
	t.Parallel()
	c := models.Call{Kind: models.CallKindVoice, SourceID: 3999999, IngressID: 777, Transport: "openbridge"}
	resp := apimodels.NewWSCallResponseFromCall(&c)
	assert.Equal(t, uint(3999999), resp.User.ID)
	assert.Empty(t, resp.User.Callsign)
	assert.Equal(t, uint(777), resp.Repeater.RadioID)
	assert.Equal(t, uint(777), resp.RepeaterID)
}
