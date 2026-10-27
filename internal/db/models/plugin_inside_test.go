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
	"crypto/sha256"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func netCheckIn() models.PluginInfo {
	return models.PluginInfo{
		Name: "Net check-in", Version: "0.4.1", Author: "N1CALL",
		Description: "Logs each radio that calls 9100 during a net and answers with a short tone.",
		Hosts: []models.PluginHost{
			{Address: "nets.example.org:443", Reason: "Posts the check-in list when a net ends."},
			{Address: "time.example.org:123", Reason: "Keeps time."},
		},
	}
}

func TestAnUploadedPluginKeepsItsModuleAndHosts(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	wasm := []byte("\x00asm\x01\x00\x00\x00 one")
	p, err := models.CreateUploadedPlugin(database, netCheckIn(), []models.PluginRange{rng(9100, 9100)}, []string{"nets.example.org:443"}, wasm, time.Now())
	require.NoError(t, err)
	assert.Equal(t, models.PluginUploaded, p.Kind)
	assert.Equal(t, "Net check-in", p.Name)
	assert.Equal(t, "0.4.1", p.Version)
	assert.Equal(t, "N1CALL", p.Author)
	assert.True(t, p.Enabled)
	require.Len(t, p.Hosts, 2)
	assert.Equal(t, "nets.example.org:443", p.Hosts[0].Address)
	assert.True(t, p.Hosts[0].Allowed)
	assert.False(t, p.Hosts[1].Allowed, "a host starts out not allowed")
	sum := sha256.Sum256(wasm)
	assert.Equal(t, sum[:], p.ModuleHash)

	hash, got, err := models.PluginModuleOf(database, p.ID)
	require.NoError(t, err)
	assert.Equal(t, wasm, got)
	assert.Equal(t, sum[:], hash)

	list, err := models.ListPlugins(database)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Len(t, list[0].Hosts, 2, "the list shows the hosts")
}

func TestANewVersionAddsItsHostsNotAllowed(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	p, err := models.CreateUploadedPlugin(database, netCheckIn(), nil, []string{"nets.example.org:443", "time.example.org:123"}, []byte("v1"), time.Now())
	require.NoError(t, err)

	next := netCheckIn()
	next.Version = "0.5.0"
	next.Hosts = []models.PluginHost{
		{Address: "nets.example.org:443", Reason: "Posts the list."},
		{Address: "new.example.org:443", Reason: "Something new."},
	}
	p, err = models.SetPluginModule(database, p.ID, next, []byte("v2"), time.Now())
	require.NoError(t, err)
	assert.Equal(t, "0.5.0", p.Version)
	require.Len(t, p.Hosts, 2)
	byAddress := map[string]models.PluginHost{}
	for _, h := range p.Hosts {
		byAddress[h.Address] = h
	}
	assert.True(t, byAddress["nets.example.org:443"].Allowed, "a host the admin allowed stays allowed")
	assert.Equal(t, "Posts the list.", byAddress["nets.example.org:443"].Reason)
	assert.False(t, byAddress["new.example.org:443"].Allowed, "a host the new version adds starts out not allowed")
	_, gone := byAddress["time.example.org:123"]
	assert.False(t, gone, "a host the new version drops is gone")
	_, wasm, err := models.PluginModuleOf(database, p.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), wasm)
}

func TestTheAdminAllowsOnlyTheHostsAPluginAsks(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	p, err := models.CreateUploadedPlugin(database, netCheckIn(), nil, nil, []byte("v1"), time.Now())
	require.NoError(t, err)
	p, err = models.SetAllowedPluginHosts(database, p.ID, []string{"time.example.org:123", "evil.example.org:25"})
	require.NoError(t, err)
	allowed := []string{}
	for _, h := range p.Hosts {
		if h.Allowed {
			allowed = append(allowed, h.Address)
		}
	}
	assert.Equal(t, []string{"time.example.org:123"}, allowed)
	_, err = models.CreateUploadedPlugin(database, netCheckIn(), nil, []string{"evil.example.org:25"}, []byte("v2"), time.Now())
	assert.ErrorIs(t, err, models.ErrPluginName, "another plugin is called Net check-in")
}

func TestABuiltinPluginIsMadeOnceWithItsDefaultIDs(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	p, err := models.EnsureBuiltinPlugin(database, "parrot", "Parrot", "v1.3.4", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, models.PluginBuiltin, p.Kind)
	assert.Equal(t, "parrot", p.Builtin)
	assert.Equal(t, "v1.3.4", p.Version)
	require.Len(t, p.Ranges, 1)

	_, err = models.UpdatePlugin(database, p.ID, "Parrot", false, []models.PluginRange{rng(9991, 9991)})
	require.NoError(t, err)
	again, err := models.EnsureBuiltinPlugin(database, "parrot", "Parrot", "v1.3.5", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, p.ID, again.ID)
	assert.Equal(t, "v1.3.5", again.Version, "the version follows DMRHub")
	assert.False(t, again.Enabled, "the admin's choices stay")
	require.Len(t, again.Ranges, 1)
	assert.Equal(t, uint(9991), again.Ranges[0].First)
}

func TestABuiltinPluginWhoseIDsAreTakenWaitsForThem(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	mine, _, err := models.CreatePlugin(database, "Old locator", []models.PluginRange{rng(5052, 5052)}, time.Now())
	require.NoError(t, err)
	p, err := models.EnsureBuiltinPlugin(database, "locator", "Location server", "v1.3.4", []models.PluginRange{rng(5050, 5059)}, time.Now())
	require.NoError(t, err)
	require.Len(t, p.Ranges, 1, "it keeps the IDs it waits for")
	list, err := models.ListPlugins(database)
	require.NoError(t, err)
	assert.Equal(t, map[uint]models.PluginWait{p.ID: {ID: 5052, By: "Old locator"}}, models.WaitingPlugins(list))

	_, err = models.UpdatePlugin(database, mine.ID, "Old locator", false, mine.Ranges)
	require.NoError(t, err)
	list, err = models.ListPlugins(database)
	require.NoError(t, err)
	assert.Empty(t, models.WaitingPlugins(list), "it has its IDs once the other plugin is off")
	_, err = models.UpdatePlugin(database, mine.ID, "Old locator", true, mine.Ranges)
	require.NoError(t, err, "a built-in plugin never keeps another plugin from its IDs")
	list, err = models.ListPlugins(database)
	require.NoError(t, err)
	assert.Equal(t, map[uint]models.PluginWait{p.ID: {ID: 5052, By: "Old locator"}}, models.WaitingPlugins(list), "it waits again")
}

func TestAnExternalPluginCanTakeTheNameAndIDsOfABuiltinPlugin(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	builtin, err := models.EnsureBuiltinPlugin(database, "parrot", "Parrot", "v1.3.4", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	require.True(t, builtin.Enabled)

	external, _, err := models.CreatePlugin(database, "Parrot", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, models.PluginExternal, external.Kind)
	list, err := models.ListPlugins(database)
	require.NoError(t, err)
	assert.Equal(t, map[uint]models.PluginWait{builtin.ID: {ID: 9990, By: "Parrot"}}, models.WaitingPlugins(list))

	_, _, err = models.CreatePlugin(database, "Parrot", nil, time.Now())
	require.ErrorIs(t, err, models.ErrPluginName, "two plugins that are not built in never share a name")
	info := netCheckIn()
	info.Name = "Parrot"
	_, err = models.CreateUploadedPlugin(database, info, nil, nil, []byte("\x00asm"), time.Now())
	require.ErrorIs(t, err, models.ErrPluginName)
	found, ok, err := models.PluginByName(database, "Parrot")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, external.ID, found.ID, "the name is the external plugin's")

	_, err = models.UpdatePlugin(database, builtin.ID, "Parrot", true, []models.PluginRange{rng(9990, 9990)})
	var taken *models.PluginIDTakenError
	require.ErrorAs(t, err, &taken, "a built-in plugin cannot take the IDs of another plugin that is on")
	assert.Equal(t, uint(9990), taken.ID)
}

func TestABuiltinPluginIsMadeBesideAnExternalOneOfItsName(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	_, _, err := models.CreatePlugin(database, "Parrot", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	p, err := models.EnsureBuiltinPlugin(database, "parrot", "Parrot", "v1.3.4", []models.PluginRange{rng(9990, 9990)}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, models.PluginBuiltin, p.Kind)
}

func TestAnExternalPluginSaysItsVersion(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	p, _, err := models.CreatePlugin(database, "Weather", nil, time.Now())
	require.NoError(t, err)
	assert.Equal(t, models.PluginExternal, p.Kind)
	require.NoError(t, models.NotePluginVersion(database, p.ID, "2.0.0"))
	p, err = models.FindPlugin(database, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", p.Version)
}

func TestDeletingAnUploadedPluginDropsItsModule(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()
	p, err := models.CreateUploadedPlugin(database, netCheckIn(), nil, nil, []byte("v1"), time.Now())
	require.NoError(t, err)
	require.NoError(t, models.DeletePlugin(database, p.ID))
	_, _, err = models.PluginModuleOf(database, p.ID)
	require.ErrorIs(t, err, models.ErrPluginNotFound)
	var hosts int64
	require.NoError(t, database.Model(&models.PluginHost{}).Where("plugin_id = ?", p.ID).Count(&hosts).Error)
	assert.Zero(t, hosts)
}
