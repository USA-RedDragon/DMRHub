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
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rng(first, last uint) models.PluginRange {
	return models.PluginRange{First: first, Last: last}
}

func TestAPluginIsFoundByItsTokenOnly(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	p, token, err := models.CreatePlugin(database, "Parrot", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(token, models.PluginTokenPrefix))
	assert.True(t, p.Enabled)
	require.Len(t, p.Ranges, 1)

	got, ok, err := models.PluginByToken(database, token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "Parrot", got.Name)
	assert.Equal(t, uint(9990), got.Ranges[0].First)
	assert.NotContains(t, string(got.TokenHash), token, "only the hash is stored")

	for _, bad := range []string{"", "nope", token + "x", models.PluginTokenPrefix} {
		_, ok, err := models.PluginByToken(database, bad)
		require.NoError(t, err)
		assert.False(t, ok, bad)
	}

	fresh, err := models.RotatePluginToken(database, p.ID, time.Now())
	require.NoError(t, err)
	_, ok, err = models.PluginByToken(database, token)
	require.NoError(t, err)
	assert.False(t, ok, "the old token stops working")
	_, ok, err = models.PluginByToken(database, fresh)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestPluginRangesAreValidAndNeverOverlap(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	a, _, err := models.CreatePlugin(database, "A", []models.PluginRange{rng(310001, 310009), rng(9998, 9998)}, time.Now())
	require.NoError(t, err)

	for name, ranges := range map[string][]models.PluginRange{
		"zero":           {rng(0, 5)},
		"backwards":      {rng(10, 5)},
		"too big":        {rng(1, models.MaxDMRID+1)},
		"overlap itself": {rng(1, 10), rng(10, 20)},
		"overlap other":  {rng(310005, 310020)},
		"inside other":   {rng(9998, 9998)},
	} {
		_, _, err := models.CreatePlugin(database, "B-"+name, ranges, time.Now())
		assert.ErrorIs(t, err, models.ErrPluginRange, name)
	}
	many := make([]models.PluginRange, models.MaxPluginRanges+1)
	for i := range many {
		many[i] = rng(uint(1000+i*10), uint(1000+i*10))
	}
	_, _, err = models.CreatePlugin(database, "many", many, time.Now())
	assert.ErrorIs(t, err, models.ErrPluginRange)

	_, err = models.UpdatePlugin(database, a.ID, "A", false, []models.PluginRange{rng(310001, 310020)})
	require.NoError(t, err, "a plugin may grow over its own IDs")
	_, _, err = models.CreatePlugin(database, "B", []models.PluginRange{rng(9998, 9998)}, time.Now())
	require.NoError(t, err, "IDs a plugin gave up are free")
	list, err := models.ListPlugins(database)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "A", list[0].Name)
	assert.False(t, list[0].Enabled)
}

func TestPluginNamesAreUnique(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	_, _, err := models.CreatePlugin(database, "Parrot", nil, time.Now())
	require.NoError(t, err)
	for _, bad := range []string{"Parrot", "", " Parrot", strings.Repeat("p", models.MaxPluginName+1)} {
		_, _, err := models.CreatePlugin(database, bad, nil, time.Now())
		assert.ErrorIs(t, err, models.ErrPluginName, bad)
	}
}

func TestDeletingAPluginFreesItsIDs(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	p, _, err := models.CreatePlugin(database, "Parrot", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	require.NoError(t, models.DeletePlugin(database, p.ID))
	assert.ErrorIs(t, models.DeletePlugin(database, p.ID), models.ErrPluginNotFound)
	_, err = models.FindPlugin(database, p.ID)
	assert.ErrorIs(t, err, models.ErrPluginNotFound)
	_, _, err = models.CreatePlugin(database, "Parrot 2", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	_, err = models.RotatePluginToken(database, 999, time.Now())
	assert.ErrorIs(t, err, models.ErrPluginNotFound)
}
