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
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func keysOf(entries []models.PluginStoreEntry) []string {
	out := make([]string, 0, len(entries))
	for i := range entries {
		out = append(out, entries[i].Key())
	}
	return out
}

func TestAPluginStoreKeepsItsKeysApartFromOtherPluginsAndInByteOrder(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, database *gorm.DB) {
		now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		for _, k := range []string{"mail/N2CALL/2", "mail/N2CALL/1", "mail/n2call/1", "mail/N0CALL/1", "other"} {
			require.NoError(t, models.PluginStorePut(database, 1, k, []byte("v "+k), nil, now))
		}
		require.NoError(t, models.PluginStorePut(database, 2, "mail/N2CALL/1", []byte("plugin 2"), nil, now))
		require.NoError(t, models.PluginStorePut(database, 1, "mail/N2CALL/1", []byte("again"), nil, now))

		got, ok, err := models.PluginStoreGet(database, 1, "mail/N2CALL/1", now)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, []byte("again"), got.Value)
		got, ok, err = models.PluginStoreGet(database, 1, "mail/n2call/1", now)
		require.NoError(t, err)
		require.True(t, ok, "keys differ by case on every database")
		assert.Equal(t, []byte("v mail/n2call/1"), got.Value)

		list, more, err := models.PluginStoreList(database, 1, "mail/", "", 0, now)
		require.NoError(t, err)
		assert.False(t, more)
		assert.Equal(t, []string{"mail/N0CALL/1", "mail/N2CALL/1", "mail/N2CALL/2", "mail/n2call/1"}, keysOf(list))

		list, more, err = models.PluginStoreList(database, 1, "mail/", "mail/N0CALL/1", 2, now)
		require.NoError(t, err)
		assert.True(t, more)
		assert.Equal(t, []string{"mail/N2CALL/1", "mail/N2CALL/2"}, keysOf(list))

		require.NoError(t, models.PluginStoreDelete(database, 1, "mail/N2CALL/1"))
		require.NoError(t, models.PluginStoreDelete(database, 1, "never there"))
		_, ok, err = models.PluginStoreGet(database, 1, "mail/N2CALL/1", now)
		require.NoError(t, err)
		assert.False(t, ok)
		_, ok, err = models.PluginStoreGet(database, 2, "mail/N2CALL/1", now)
		require.NoError(t, err)
		assert.True(t, ok, "another plugin keeps its key")
	})
}

func TestAPluginStoreKeyExpires(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, database *gorm.DB) {
		now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		at := now.Add(time.Hour)
		require.NoError(t, models.PluginStorePut(database, 1, "k", []byte("v"), &at, now))
		got, ok, err := models.PluginStoreGet(database, 1, "k", now.Add(59*time.Minute))
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, got.ExpiresAt)
		assert.True(t, got.ExpiresAt.Equal(at))

		later := now.Add(time.Hour)
		_, ok, err = models.PluginStoreGet(database, 1, "k", later)
		require.NoError(t, err)
		assert.False(t, ok)
		list, _, err := models.PluginStoreList(database, 1, "", "", 0, later)
		require.NoError(t, err)
		assert.Empty(t, list)

		require.NoError(t, models.PluginStorePut(database, 1, "other", nil, nil, later))
		var n int64
		require.NoError(t, database.Model(&models.PluginStoreEntry{}).Where("plugin_id = ?", 1).Count(&n).Error)
		assert.Equal(t, int64(1), n, "a put drops the expired keys")
	})
}

func TestAPluginStoreRefusesBadKeysLargeValuesAndANewKeyWhenFull(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	now := time.Now()
	require.ErrorIs(t, models.PluginStorePut(database, 1, "", nil, nil, now), models.ErrPluginStoreKey)
	require.ErrorIs(t, models.PluginStorePut(database, 1, string(make([]byte, models.MaxPluginStoreKey+1)), nil, nil, now), models.ErrPluginStoreKey)
	require.ErrorIs(t, models.PluginStorePut(database, 1, "k", make([]byte, models.MaxPluginStoreValue+1), nil, now), models.ErrPluginStoreValue)
	require.NoError(t, models.PluginStorePut(database, 1, "k", make([]byte, models.MaxPluginStoreValue), nil, now))

	rows := make([]models.PluginStoreEntry, 0, models.MaxPluginStoreKeys)
	for i := 1; i < models.MaxPluginStoreKeys; i++ {
		rows = append(rows, models.PluginStoreEntry{PluginID: 1, KeyHex: fmt.Sprintf("%x", fmt.Sprintf("n%05d", i)), Value: []byte{}})
	}
	require.NoError(t, database.CreateInBatches(rows, 500).Error)
	require.ErrorIs(t, models.PluginStorePut(database, 1, "new", nil, nil, now), models.ErrPluginStoreFull)
	require.NoError(t, models.PluginStorePut(database, 1, "k", []byte("an old key still changes"), nil, now))
	require.NoError(t, models.PluginStorePut(database, 2, "new", nil, nil, now), "the limit is per plugin")
}

func aprsSwitch() models.PluginSwitchSpec {
	return models.PluginSwitchSpec{
		Key: "aprs", Title: "APRS messages", Description: "Send and get APRS text messages with your radio.",
		Notice:  "APRS messages go out on the internet, where anyone can read them.",
		Details: []models.PluginSwitchDetail{{Label: "Your APRS address", Value: "{callsign}-{choice}"}},
		Choice:  &models.PluginSwitchChoice{Label: "SSID", Options: []string{"5", "7", "9"}, Preset: "7"},
	}
}

func addUser(t *testing.T, database *gorm.DB, id uint, call string, approved, suspended bool) models.User {
	t.Helper()
	u := models.User{ID: id, Callsign: call, Username: call, Approved: approved, Suspended: suspended}
	require.NoError(t, database.Create(&u).Error)
	return u
}

func TestAPluginDeclaresValidSwitchesOnly(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	p, _, err := models.CreatePlugin(database, "APRS", []models.PluginRange{rng(4700, 4700)}, time.Now())
	require.NoError(t, err)
	ok := aprsSwitch()
	bad := []func(s *models.PluginSwitchSpec){
		func(s *models.PluginSwitchSpec) { s.Key = "" },
		func(s *models.PluginSwitchSpec) { s.Key = "APRS" },
		func(s *models.PluginSwitchSpec) { s.Title = "" },
		func(s *models.PluginSwitchSpec) { s.Choice.Options = nil },
		func(s *models.PluginSwitchSpec) { s.Choice.Options = []string{"7", "7"} },
		func(s *models.PluginSwitchSpec) { s.Choice.Preset = "15" },
		func(s *models.PluginSwitchSpec) { s.Details = make([]models.PluginSwitchDetail, 5) },
	}
	for i, f := range bad {
		s := aprsSwitch()
		c := *s.Choice
		s.Choice = &c
		f(&s)
		require.ErrorIs(t, models.SetPluginSwitches(database, p.ID, []models.PluginSwitchSpec{s}), models.ErrPluginSwitch, i)
	}
	require.ErrorIs(t, models.SetPluginSwitches(database, p.ID, []models.PluginSwitchSpec{ok, ok}), models.ErrPluginSwitch)
	five := make([]models.PluginSwitchSpec, 5)
	for i := range five {
		five[i] = models.PluginSwitchSpec{Key: fmt.Sprintf("s%d", i), Title: "S"}
	}
	require.ErrorIs(t, models.SetPluginSwitches(database, p.ID, five), models.ErrPluginSwitch)

	require.NoError(t, models.SetPluginSwitches(database, p.ID, []models.PluginSwitchSpec{ok, {Key: "notice", Title: "Mail notice"}}))
	got, err := models.PluginSwitchesOf(database, p.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, ok, got[0])
	assert.Equal(t, "notice", got[1].Key)
	require.ErrorIs(t, models.SetPluginSwitches(database, p.ID+1, []models.PluginSwitchSpec{ok}), models.ErrPluginNotFound)
}

func TestAUserSetsAPluginSwitchAndThePluginSeesTheUsersWhoTurnedItOn(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, database *gorm.DB) {
		now := time.Now()
		p, _, err := models.CreatePlugin(database, "APRS", []models.PluginRange{rng(4700, 4700)}, now)
		require.NoError(t, err)
		require.NoError(t, models.SetPluginSwitches(database, p.ID, []models.PluginSwitchSpec{aprsSwitch()}))
		kp := addUser(t, database, 1000001, "N1CALL", true, false)
		w1 := addUser(t, database, 1000002, "N2CALL", true, false)
		addUser(t, database, 1000003, "N7CALL", true, true)
		addUser(t, database, 1000004, "N6CALL", false, false)

		mine, err := models.UserPluginSwitches(database, p.ID, &kp)
		require.NoError(t, err)
		require.Len(t, mine, 1)
		assert.False(t, mine[0].On)
		assert.Equal(t, "7", mine[0].Choice, "the preset before the user chooses")

		_, err = models.SetUserPluginSwitch(database, p.ID, kp.ID, "aprs", true, "15", now)
		require.ErrorIs(t, err, models.ErrPluginSwitch)
		_, err = models.SetUserPluginSwitch(database, p.ID, kp.ID, "nope", true, "", now)
		require.ErrorIs(t, err, models.ErrPluginSwitch)
		set, err := models.SetUserPluginSwitch(database, p.ID, kp.ID, "aprs", true, "9", now)
		require.NoError(t, err)
		assert.Equal(t, "N1CALL", set.Callsign)
		assert.Equal(t, "9", set.Choice)
		_, err = models.SetUserPluginSwitch(database, p.ID, w1.ID, "aprs", true, "", now)
		require.NoError(t, err)
		for _, id := range []uint{1000003, 1000004} {
			_, err = models.SetUserPluginSwitch(database, p.ID, id, "aprs", true, "", now)
			require.NoError(t, err)
		}

		on, err := models.PluginSwitchesOn(database, p.ID)
		require.NoError(t, err)
		require.Len(t, on, 2, "only approved users who are not suspended")
		assert.Equal(t, "N1CALL", on[0].Callsign)
		assert.Equal(t, "9", on[0].Choice)
		assert.Equal(t, "N2CALL", on[1].Callsign)
		assert.Equal(t, "7", on[1].Choice)

		_, err = models.SetUserPluginSwitch(database, p.ID, kp.ID, "aprs", false, "9", now)
		require.NoError(t, err)
		on, err = models.PluginSwitchesOn(database, p.ID)
		require.NoError(t, err)
		require.Len(t, on, 1)

		all, err := models.ListUserPluginSwitches(database, &kp)
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, "APRS", all[0].Plugin.Name)
		require.NotEmpty(t, all[0].Plugin.Ranges)
		assert.Equal(t, "9", all[0].Choice, "the choice stays while the switch is off")

		_, err = models.UpdatePlugin(database, p.ID, p.Name, false, p.Ranges)
		require.NoError(t, err)
		all, err = models.ListUserPluginSwitches(database, &kp)
		require.NoError(t, err)
		assert.Empty(t, all, "a plugin that is off shows no switch")

		require.NoError(t, models.DeletePlugin(database, p.ID))
		var n int64
		require.NoError(t, database.Model(&models.PluginUserSwitch{}).Count(&n).Error)
		assert.Zero(t, n)
	})
}

func TestTwoReplicasSaveTheSameSwitchesOfAPluginAtOnce(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, database *gorm.DB) {
		specs := []models.PluginSwitchSpec{aprsSwitch(), {Key: "notice", Title: "Mail notice"}}
		for round := range 20 {
			p, _, err := models.CreatePlugin(database, fmt.Sprintf("Plugin %d", round), []models.PluginRange{rng(uint(4700+round), uint(4700+round))}, time.Now())
			require.NoError(t, err)
			plugin := p.ID
			start := make(chan struct{})
			errs := make(chan error, 2)
			for range 2 {
				go func() {
					<-start
					errs <- models.SetPluginSwitches(database, plugin, specs)
				}()
			}
			close(start)
			for range 2 {
				require.NoError(t, <-errs, "round %d", round)
			}
			got, err := models.PluginSwitchesOf(database, plugin)
			require.NoError(t, err)
			assert.Equal(t, specs, got)
		}
	})
}
