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
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const mustChangeBody = `{"error":"An admin asked you to choose a new password. Choose your own password in Settings first.","must_change_password":true}`

func requirePath(id uint) string {
	return fmt.Sprintf("/api/v1/users/%d/require-password-change", id)
}

func requireRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	return router, tdb.DB()
}

func requireChange(t *testing.T, router *gin.Engine, jar testutils.CookieJar, id uint, generate bool) (int, string) {
	t.Helper()
	w := testutils.Do(t, router, jar, http.MethodPost, requirePath(id), map[string]bool{"generate_password": generate})
	var out struct {
		Password string `json:"password"`
	}
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	}
	return w.Code, out.Password
}

func TestRequirePasswordChangeKeepsThePassword(t *testing.T) {
	t.Parallel()
	router, db := requireRouter(t)
	user := testutils.SeedAccount(t, db, 1000001, "N0CALL", "n0call")
	first := testutils.Login(t, router, "n0call")
	second := testutils.Login(t, router, "n0call")
	admin := testutils.Login(t, router, "Admin")

	w := testutils.Do(t, router, admin, http.MethodPost, requirePath(user.ID), map[string]bool{"generate_password": false})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"message":"The user must choose a new password"}`, w.Body.String())

	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, first, http.MethodGet, "/api/v1/users/me", nil).Code, "every session ends")
	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, second, http.MethodGet, "/api/v1/users/me", nil).Code, "every session ends")
	assert.Equal(t, http.StatusOK, testutils.Do(t, router, admin, http.MethodGet, "/api/v1/users/me/settings", nil).Code, "the admin stays logged in")

	jar, code := loginWith(t, router, "n0call", testutils.TestPassword)
	require.Equal(t, http.StatusOK, code, "the old password still logs in")
	assert.True(t, mustChange(t, router, jar))
	w = testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me/settings", nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.JSONEq(t, mustChangeBody, w.Body.String())

	w = testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": testutils.TestPassword, "current_password": testutils.TestPassword})
	assert.Equal(t, http.StatusBadRequest, w.Code, "the same password is refused")
	assert.True(t, mustChange(t, router, jar))

	w = testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": "my-own-password", "current_password": testutils.TestPassword})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, mustChange(t, router, jar))
	assert.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me/settings", nil).Code)
}

func TestRequirePasswordChangeGeneratesAPassword(t *testing.T) {
	t.Parallel()
	router, db := requireRouter(t)
	user := testutils.SeedAccount(t, db, 1000001, "N0CALL", "n0call")
	old := testutils.Login(t, router, "n0call")
	admin := testutils.Login(t, router, "Admin")

	w := testutils.Do(t, router, admin, http.MethodPost, requirePath(user.ID), map[string]bool{"generate_password": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var out struct {
		Message  string `json:"message"`
		Password string `json:"password"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "The user must choose a new password", out.Message)
	require.Len(t, out.Password, utils.TemporaryPasswordLength)
	for _, r := range out.Password {
		require.True(t, strings.ContainsRune(utils.TemporaryPasswordAlphabet, r), "%q", r)
	}
	var stored models.User
	require.NoError(t, db.First(&stored, user.ID).Error)
	assert.NotContains(t, stored.Password, out.Password, "only the hash is stored")

	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, old, http.MethodGet, "/api/v1/users/me", nil).Code)
	_, code := loginWith(t, router, "n0call", testutils.TestPassword)
	assert.Equal(t, http.StatusUnauthorized, code, "the old password stops working")

	jar, code := loginWith(t, router, "n0call", out.Password)
	require.Equal(t, http.StatusOK, code, "the new password logs in")
	assert.True(t, mustChange(t, router, jar))
	w = testutils.Do(t, router, jar, http.MethodGet, "/api/v1/repeaters/my", nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.JSONEq(t, mustChangeBody, w.Body.String())

	w = testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": "my-own-password", "current_password": out.Password})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, mustChange(t, router, jar))

	_, second := requireChange(t, router, admin, user.ID, true)
	assert.NotEqual(t, out.Password, second, "each call makes a new password")
}

func TestRequirePasswordChangeFollowsTheAdminRules(t *testing.T) {
	t.Parallel()
	router, db := requireRouter(t)
	testutils.SeedAccount(t, db, 1000001, "N0CALL", "admin1")
	other := testutils.SeedAccount(t, db, 1000002, "N1CALL", "admin2")
	user := testutils.SeedAccount(t, db, 1000003, "N2CALL", "user3")
	testutils.SeedAccount(t, db, 1000004, "N3CALL", "user4")
	require.NoError(t, db.Model(&models.User{}).Where("id IN ?", []uint{1000001, 1000002}).Update("admin", true).Error)
	var super models.User
	require.NoError(t, db.Where("username = ?", "Admin").First(&super).Error)
	admin := testutils.Login(t, router, "admin1")
	nobody := testutils.Login(t, router, "user4")

	code, _ := requireChange(t, router, nobody, user.ID, true)
	assert.Equal(t, http.StatusForbidden, code, "a user who is not an admin")
	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, requirePath(user.ID), map[string]bool{}).Code, "no login")

	for _, target := range []uint{other.ID, super.ID} {
		w := testutils.Do(t, router, admin, http.MethodPost, requirePath(target), map[string]bool{"generate_password": true})
		assert.Equal(t, http.StatusBadRequest, w.Code, "an admin on %d", target)
		assert.JSONEq(t, `{"error":"Only the System Admin can reset the password of an admin."}`, w.Body.String())
	}
	_, c := loginWith(t, router, "admin2", testutils.TestPassword)
	assert.Equal(t, http.StatusOK, c, "the refused call must not change the password")
	var stored models.User
	require.NoError(t, db.First(&stored, other.ID).Error)
	assert.False(t, stored.MustChangePassword, "the refused call must not set the flag")

	w := testutils.Do(t, router, admin, http.MethodPost, requirePath(1000001), map[string]bool{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"You cannot do this to yourself. Change your password in Settings."}`, w.Body.String())

	code, _ = requireChange(t, router, admin, user.ID, false)
	assert.Equal(t, http.StatusOK, code, "an admin on a user")

	sys := testutils.Login(t, router, "Admin")
	code, _ = requireChange(t, router, sys, other.ID, false)
	assert.Equal(t, http.StatusOK, code, "the System Admin on an admin")

	w = testutils.Do(t, router, sys, http.MethodPost, requirePath(9999999), map[string]bool{"generate_password": true})
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.JSONEq(t, `{"error":"User does not exist"}`, w.Body.String())
	assert.Equal(t, http.StatusBadRequest, testutils.Do(t, router, sys, http.MethodPost, "/api/v1/users/abc/require-password-change", map[string]bool{}).Code)
}
