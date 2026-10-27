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

package users_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allBackends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

type patchAnswer struct {
	Message         string `json:"message"`
	Error           string `json:"error"`
	RadioIDMismatch bool   `json:"radio_id_mismatch"`
	RadioIDCallsign string `json:"radio_id_callsign"`
}

func patchAnswerOf(t *testing.T, body []byte) patchAnswer {
	t.Helper()
	var a patchAnswer
	require.NoError(t, json.Unmarshal(body, &a))
	return a
}

func TestPATCHUserCallsign(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
				be.Migrated().Setup(t, c)
				c.DMR.DisableRadioIDValidation = false
			})
			require.NoError(t, err)
			defer tdb.CloseDB()
			database := tdb.DB()

			user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
			_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
			_, _, adminJar := testutils.LoginAdmin(t, router)
			require.NoError(t, database.Model(&models.User{}).Where("id = ?", user.DMRId).UpdateColumn("callsign", "OLDCALL").Error)

			stored := func() models.User {
				t.Helper()
				u, err := models.FindUserWithoutRepeaters(database, user.DMRId)
				require.NoError(t, err)
				return u
			}

			_, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "N0CALL"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			a := patchAnswerOf(t, w.Body.Bytes())
			assert.Equal(t, "Callsign does not match DMR ID", a.Error)
			assert.False(t, a.RadioIDMismatch, "only an admin can override")

			_, w = testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "N0CALL", OverrideRadioID: true})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "OLDCALL", stored().Callsign)

			resp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "ki5vmf"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			assert.Equal(t, "KI5VMF", stored().Callsign)

			_, w = testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{Callsign: "N0CALL"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			a = patchAnswerOf(t, w.Body.Bytes())
			assert.True(t, a.RadioIDMismatch)
			assert.Equal(t, "KI5VMF", a.RadioIDCallsign)
			assert.Equal(t, "KI5VMF", stored().Callsign)

			resp, w = testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{Callsign: "n0call", OverrideRadioID: true})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			u := stored()
			assert.Equal(t, "N0CALL", u.Callsign)
			assert.True(t, u.CallsignOverride)

			resp, w = testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{Username: "renamed"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			assert.True(t, stored().CallsignOverride, "an edit that keeps the callsign keeps the override")

			resp, w = testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{Callsign: "KI5VMF"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			u = stored()
			assert.Equal(t, "KI5VMF", u.Callsign)
			assert.False(t, u.CallsignOverride)
		})
	}
}

func TestPATCHUserCallsignWithoutRadioIDValidation(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) { be.Migrated().Setup(t, c) })
			require.NoError(t, err)
			defer tdb.CloseDB()
			database := tdb.DB()

			user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
			_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
			require.NoError(t, database.Create(&models.User{ID: 3140001, Callsign: "W1AW", Username: "other", Approved: true}).Error)

			resp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "n0call"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			u, err := models.FindUserWithoutRepeaters(database, user.DMRId)
			require.NoError(t, err)
			assert.Equal(t, "N0CALL", u.Callsign)
			assert.False(t, u.CallsignOverride)

			resp, w = testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "w1aw"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "Callsign is already taken", resp.Error)
		})
	}
}

func TestPATCHUserUsernameIsChecked(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	for name, want := range map[string]string{
		"ab":                     "Username must be at least 3 characters",
		"has space":              "Username must be alphanumeric, _, -, or .",
		"abcdefghijklmnopqrstuv": "Username must be less than 20 characters",
	} {
		resp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Username: name})
		assert.Equal(t, http.StatusBadRequest, w.Code, name)
		assert.Equal(t, want, resp.Error, name)
	}
	resp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Username: "new.name_1"})
	assert.Equal(t, http.StatusOK, w.Code, resp.Error)
}
