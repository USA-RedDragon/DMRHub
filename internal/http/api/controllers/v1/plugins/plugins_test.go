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

package plugins_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type env struct {
	t      *testing.T
	router *gin.Engine
	db     *gorm.DB
	kv     kv.KV
	admin  testutils.CookieJar
}

func newEnv(t *testing.T) *env {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		c.HTTP.CanonicalHost = "https://dmrhub.example.org"
		c.DMR.Plugins.Enabled = true
		c.DMR.Plugins.Port = 3006
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	return &env{t: t, router: router, db: tdb.DB(), kv: tdb.KV(), admin: testutils.Login(t, router, "Admin")}
}

func (e *env) do(jar testutils.CookieJar, method, path string, body any, out any) int {
	e.t.Helper()
	w := testutils.Do(e.t, e.router, jar, method, path, body)
	if out != nil && w.Code < 300 {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), out), w.Body.String())
	}
	return w.Code
}

func TestOnlyAdminsManagePlugins(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	testutils.SeedAccount(t, e.db, 1000001, "KI5VMF", "user")
	user := testutils.Login(t, e.router, "user")
	assert.Equal(t, http.StatusForbidden, e.do(user, http.MethodGet, "/api/v1/admin/plugins", nil, nil), "RequireAdmin answers 403 to users who are not admins")
	assert.Equal(t, http.StatusForbidden, e.do(user, http.MethodPost, "/api/v1/admin/plugins", map[string]any{"name": "x"}, nil))
	assert.Equal(t, http.StatusUnauthorized, e.do(testutils.CookieJar{}, http.MethodGet, "/api/v1/admin/plugins", nil, nil))
}

func TestAddingAPluginShowsItsTokenOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var made apimodels.PluginToken
	require.Equal(t, http.StatusCreated, e.do(e.admin, http.MethodPost, "/api/v1/admin/plugins",
		map[string]any{"name": "Parrot", "ranges": []map[string]uint{{"first": 9990, "last": 9990}}}, &made))
	assert.True(t, strings.HasPrefix(made.Token, models.PluginTokenPrefix))
	assert.Equal(t, "Parrot", made.Plugin.Name)
	assert.True(t, made.Plugin.Enabled)
	assert.False(t, made.Plugin.Online)

	w := testutils.Do(t, e.router, e.admin, http.MethodGet, "/api/v1/admin/plugins", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), made.Token, "the list never shows a token")
	var list apimodels.PluginList
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Plugins, 1)
	assert.Equal(t, []apimodels.PluginRange{{First: 9990, Last: 9990}}, list.Plugins[0].Ranges)
	assert.Equal(t, "dmrhub.example.org:3006", list.Connection.Address)
	assert.True(t, list.Connection.Enabled)
	assert.True(t, list.Connection.OwnCertificate)
	assert.Len(t, list.Connection.Fingerprint, 32*3-1)
}

func TestPluginInputIsChecked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	require.Equal(t, http.StatusCreated, e.do(e.admin, http.MethodPost, "/api/v1/admin/plugins",
		map[string]any{"name": "Parrot", "ranges": []map[string]uint{{"first": 9990, "last": 9990}}}, nil))
	for name, body := range map[string]map[string]any{
		"same name":  {"name": "Parrot"},
		"no name":    {"name": ""},
		"overlap":    {"name": "B", "ranges": []map[string]uint{{"first": 9980, "last": 9995}}},
		"backwards":  {"name": "C", "ranges": []map[string]uint{{"first": 10, "last": 5}}},
		"out of DMR": {"name": "D", "ranges": []map[string]uint{{"first": 1, "last": 16777216}}},
	} {
		assert.Equal(t, http.StatusBadRequest, e.do(e.admin, http.MethodPost, "/api/v1/admin/plugins", body, nil), name)
	}
}

func TestChangingAPluginAndItsToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var made apimodels.PluginToken
	require.Equal(t, http.StatusCreated, e.do(e.admin, http.MethodPost, "/api/v1/admin/plugins",
		map[string]any{"name": "Parrot", "ranges": []map[string]uint{{"first": 9990, "last": 9990}}}, &made))
	path := "/api/v1/admin/plugins/" + jsonID(made.Plugin.ID)

	var got apimodels.AdminPlugin
	require.Equal(t, http.StatusOK, e.do(e.admin, http.MethodPut, path,
		map[string]any{"name": "Echo", "enabled": false, "ranges": []map[string]uint{{"first": 9990, "last": 9991}}}, &got))
	assert.Equal(t, "Echo", got.Name)
	assert.False(t, got.Enabled)
	assert.Equal(t, uint(9991), got.Ranges[0].Last)
	assert.Equal(t, http.StatusBadRequest, e.do(e.admin, http.MethodPut, path, map[string]any{"name": "Echo"}, nil), "enabled is required")

	var fresh apimodels.PluginToken
	require.Equal(t, http.StatusOK, e.do(e.admin, http.MethodPost, path+"/token", nil, &fresh))
	assert.NotEqual(t, made.Token, fresh.Token)
	_, ok, err := models.PluginByToken(e.db, made.Token)
	require.NoError(t, err)
	assert.False(t, ok, "the old token stops working")

	assert.Equal(t, http.StatusNoContent, e.do(e.admin, http.MethodDelete, path, nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(e.admin, http.MethodDelete, path, nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(e.admin, http.MethodPost, path+"/token", nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(e.admin, http.MethodPut, "/api/v1/admin/plugins/x", map[string]any{"name": "a", "enabled": true}, nil))
}

func TestAConnectedPluginShowsWhereFromAndItsCallsToday(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	p, _, err := models.CreatePlugin(e.db, "Parrot", []models.PluginRange{{First: 9990, Last: 9990}}, time.Now())
	require.NoError(t, err)
	require.NoError(t, models.NotePluginConnected(e.db, p.ID, "s1", "10.0.4.7:51234", time.Now().Add(-2*time.Hour)))
	require.NoError(t, models.NotePluginSeen(e.db, p.ID, "s1", time.Now()))
	require.NoError(t, e.db.Create(&models.Call{StreamID: 1, StartTime: time.Now(), DestinationID: 9990}).Error)
	var list apimodels.PluginList
	require.Equal(t, http.StatusOK, e.do(e.admin, http.MethodGet, "/api/v1/admin/plugins", nil, &list))
	require.Len(t, list.Plugins, 1)
	got := list.Plugins[0]
	assert.True(t, got.Online)
	assert.Equal(t, "10.0.4.7:51234", got.Address)
	require.NotNil(t, got.ConnectedSince)
	assert.WithinDuration(t, time.Now().Add(-2*time.Hour), *got.ConnectedSince, time.Minute)
	assert.Equal(t, int64(1), got.CallsToday)
	assert.Equal(t, "plugin:"+jsonID(p.ID), plugins.SourceName(p.ID))
}

func jsonID(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
