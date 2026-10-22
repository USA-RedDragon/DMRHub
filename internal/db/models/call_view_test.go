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
	"sort"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type idWindow struct{ ids []uint }

func (w idWindow) VisibleSMS(db *gorm.DB, _ *models.User) *gorm.DB {
	return db.Model(&models.Call{}).Select("id").Where("id IN ?", w.ids)
}

func seedKinds(t *testing.T, database *gorm.DB) map[string]uint {
	t.Helper()
	rows := []struct {
		name string
		kind models.CallKind
		sub  string
	}{
		{"voice", models.CallKindVoice, ""},
		{"lrrp", models.CallKindData, "lrrp"},
		{"sms1", models.CallKindData, models.SubKindSMS},
		{"sms2", models.CallKindData, models.SubKindSMS},
		{"gps", models.CallKindGPS, ""},
		{"alias", models.CallKindAlias, ""},
		{"csbk", models.CallKindCSBK, "preamble"},
		{"udt", models.CallKindUDT, ""},
		{"mbc", models.CallKindMBC, ""},
		{"idle", models.CallKindIdle, ""},
		{"usbd", models.CallKindUSBD, "0"},
		{"unknown", models.CallKindUnknown, "reserved"},
	}
	ids := map[string]uint{}
	for i, r := range rows {
		c := models.Call{StreamID: uint(i + 1), StartTime: time.Now(), Kind: r.kind, SubKind: r.sub, GroupCall: true, DestinationID: 91}
		require.NoError(t, database.Create(&c).Error)
		ids[r.name] = c.ID
	}
	return ids
}

func names(t *testing.T, database *gorm.DB, ids map[string]uint, f models.CallFilter, viewer *models.User, w models.SMSWindow) []string {
	t.Helper()
	var calls []models.Call
	require.NoError(t, database.Scopes(f.Scope(database, viewer, w)).Find(&calls).Error)
	byID := map[uint]string{}
	for n, id := range ids {
		byID[id] = n
	}
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, byID[c.ID])
	}
	sort.Strings(out)
	return out
}

func TestCallFilterDefaultHidesUnknownAndIdle(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	ids := seedKinds(t, database)

	got := names(t, database, ids, models.CallFilter{}, nil, models.MaskedSMSWindow{})
	assert.Equal(t, []string{"alias", "csbk", "gps", "lrrp", "mbc", "sms1", "sms2", "udt", "voice"}, got)

	got = names(t, database, ids, models.CallFilter{Types: []string{models.TypeControl}, Idle: true}, nil, models.MaskedSMSWindow{})
	assert.Equal(t, []string{"idle", "mbc", "udt", "usbd"}, got)

	got = names(t, database, ids, models.CallFilter{Types: []string{models.TypeUnknown}}, nil, models.MaskedSMSWindow{})
	assert.Equal(t, []string{"unknown"}, got)
}

func TestCallFilterCountsHiddenSMSAsData(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	ids := seedKinds(t, database)
	user := &models.User{ID: 3191868, Approved: true}
	admin := &models.User{ID: 1, Approved: true, Admin: true}
	window := idWindow{ids: []uint{ids["sms1"]}}
	sms := models.CallFilter{Types: []string{models.TypeSMS}}
	dataOnly := models.CallFilter{Types: []string{models.TypeData}}

	assert.Equal(t, []string{"sms1"}, names(t, database, ids, sms, user, window))
	assert.Equal(t, []string{"lrrp", "sms2"}, names(t, database, ids, dataOnly, user, window), "an SMS outside the window is data")

	assert.Empty(t, names(t, database, ids, sms, user, models.MaskedSMSWindow{}))
	assert.Equal(t, []string{"lrrp", "sms1", "sms2"}, names(t, database, ids, dataOnly, nil, models.MaskedSMSWindow{}))

	assert.Equal(t, []string{"sms1", "sms2"}, names(t, database, ids, sms, admin, models.MaskedSMSWindow{}), "an admin sees every SMS")
	assert.Equal(t, []string{"lrrp"}, names(t, database, ids, dataOnly, admin, models.MaskedSMSWindow{}))

	visible, err := models.VisibleSMSIDs(database, user, window, []uint{ids["sms1"], ids["sms2"]})
	require.NoError(t, err)
	assert.Equal(t, map[uint]bool{ids["sms1"]: true}, visible)
}

func TestParseCallFilter(t *testing.T) {
	t.Parallel()
	f, err := models.ParseCallFilter("voice, sms,control", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"voice", "sms", "control"}, f.Types)
	assert.True(t, f.Idle)

	f, err = models.ParseCallFilter("", false)
	require.NoError(t, err)
	assert.Nil(t, f.Types)

	_, err = models.ParseCallFilter("voice,secret", false)
	assert.Error(t, err)
}
