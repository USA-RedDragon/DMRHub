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

package http_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type pluginSwitchJSON struct {
	PluginID    uint   `json:"plugin_id"`
	Plugin      string `json:"plugin"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Notice      string `json:"notice"`
	Details     []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"details"`
	Choice *struct {
		Label   string   `json:"label"`
		Options []string `json:"options"`
		Value   string   `json:"value"`
	} `json:"choice"`
	On bool `json:"on"`
}

func mySwitches(t *testing.T, e *oauthEnv, jar testutils.CookieJar) []pluginSwitchJSON {
	t.Helper()
	w := testutils.Do(t, e.router, jar, http.MethodGet, "/api/v1/users/me/plugin-switches", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out []pluginSwitchJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func TestAUserSetsTheSwitchOfAPluginAndThePluginHearsOfIt(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	db := e.tdb.DB()
	testutils.SeedAccount(t, db, 1000001, "N1CALL", "n1call")
	jar := testutils.Login(t, e.router, "n1call")
	p, _, err := models.CreatePlugin(db, "APRS", []models.PluginRange{{First: 4700, Last: 4709}}, time.Now())
	require.NoError(t, err)
	require.NoError(t, models.SetPluginSwitches(db, p.ID, []models.PluginSwitchSpec{{
		Key: "aprs", Title: "APRS messages", Description: "Send and get APRS text messages with your radio.",
		Notice: "APRS messages go out on the internet, where anyone can read them.",
		Details: []models.PluginSwitchDetail{
			{Label: "Your APRS address", Value: "{callsign}-{choice}"},
			{Label: "To send", Value: "Text {id} with the address and the message"},
		},
		Choice: &models.PluginSwitchChoice{Label: "SSID", Options: []string{"5", "7", "9"}, Preset: "7"},
	}}))

	got := mySwitches(t, e, jar)
	require.Len(t, got, 1)
	assert.Equal(t, "APRS", got[0].Plugin)
	assert.False(t, got[0].On)
	require.Len(t, got[0].Details, 2)
	assert.Equal(t, "N1CALL-7", got[0].Details[0].Value)
	assert.Equal(t, "Text 4700 with the address and the message", got[0].Details[1].Value)
	require.NotNil(t, got[0].Choice)
	assert.Equal(t, "7", got[0].Choice.Value)

	sub := e.tdb.PubSub().Subscribe(plugins.SwitchTopic)
	defer func() { _ = sub.Close() }()
	path := "/api/v1/users/me/plugin-switches/" + strconv.FormatUint(uint64(p.ID), 10) + "/aprs"
	w := testutils.Do(t, e.router, jar, http.MethodPut, path, map[string]any{"on": true, "choice": "9"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var put pluginSwitchJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &put))
	assert.True(t, put.On)
	assert.Equal(t, "N1CALL-9", put.Details[0].Value)

	select {
	case b := <-sub.Channel():
		require.Greater(t, len(b), 4)
		var u pluginv1.UserSwitch
		require.NoError(t, proto.Unmarshal(b[4:], &u))
		assert.True(t, proto.Equal(&pluginv1.UserSwitch{UserId: 1000001, Callsign: "N1CALL", Key: "aprs", On: true, Choice: "9"}, &u), "got %v", &u)
	default:
		t.Fatal("the plugin did not hear of the switch before the answer")
	}
	assert.True(t, mySwitches(t, e, jar)[0].On)

	for _, bad := range []struct {
		path string
		body map[string]any
		code int
	}{
		{path, map[string]any{"choice": "9"}, http.StatusBadRequest},
		{path, map[string]any{"on": true, "choice": "15"}, http.StatusNotFound},
		{"/api/v1/users/me/plugin-switches/" + strconv.FormatUint(uint64(p.ID), 10) + "/nope", map[string]any{"on": true}, http.StatusNotFound},
		{"/api/v1/users/me/plugin-switches/999/aprs", map[string]any{"on": true}, http.StatusNotFound},
		{"/api/v1/users/me/plugin-switches/x/aprs", map[string]any{"on": true}, http.StatusNotFound},
	} {
		w := testutils.Do(t, e.router, jar, http.MethodPut, bad.path, bad.body)
		assert.Equal(t, bad.code, w.Code, "%s %v: %s", bad.path, bad.body, w.Body.String())
	}

	_, err = models.UpdatePlugin(db, p.ID, p.Name, false, p.Ranges)
	require.NoError(t, err)
	assert.Empty(t, mySwitches(t, e, jar), "a plugin that is off shows no switch")
	w = testutils.Do(t, e.router, jar, http.MethodPut, path, map[string]any{"on": false})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTheSwitchesOfPluginsNeedALogin(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	w := testutils.Do(t, e.router, testutils.CookieJar{}, http.MethodGet, "/api/v1/users/me/plugin-switches", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
