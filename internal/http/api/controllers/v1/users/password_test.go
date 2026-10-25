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
	"crypto/sha1" //#nosec G505 -- HIBP range queries take SHA-1
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const breachedPassword = "breached-password"

type fakeHIBP struct{}

func (fakeHIBP) RoundTrip(r *http.Request) (*http.Response, error) {
	sum := sha1.Sum([]byte(breachedPassword)) //#nosec G401 -- HIBP range queries take SHA-1
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	body := "0000000000000000000000000000000000A:1\r\n"
	if strings.HasSuffix(r.URL.Path, "/"+hash[:5]) {
		body += hash[5:] + ":42\r\n"
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func hibpRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) {
		c.HIBPAPIKey = "test-key"
		c.AutoApprove = true
	}, dmrhttp.WithHIBPClient(&http.Client{Transport: fakeHIBP{}}))
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	return router, tdb.DB()
}

func path(id uint) string {
	return fmt.Sprintf("/api/v1/users/%d", id)
}

func TestRegisterRefusesABreachedPassword(t *testing.T) {
	t.Parallel()
	router, _ := hibpRouter(t)

	resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "breached", Password: breachedPassword})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "Password has been reported in a data breach. Please use another one", resp.Error)

	resp, w = testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "fine", Password: "a-fine-password"})
	assert.Equal(t, http.StatusOK, w.Code, resp.Error)
}

func TestPasswordChangeRefusesABreachedPassword(t *testing.T) {
	t.Parallel()
	router, db := hibpRouter(t)
	user := testutils.SeedAccount(t, db, 1000001, "N0CALL", "n0call")
	jar := testutils.Login(t, router, "n0call")

	w := testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": breachedPassword, "current_password": testutils.TestPassword})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Password has been reported in a data breach. Please use another one")
	testutils.Login(t, router, "n0call")

	admin := testutils.Login(t, router, "Admin")
	w = testutils.Do(t, router, admin, http.MethodPatch, path(user.ID), map[string]string{"password": breachedPassword})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	testutils.Login(t, router, "n0call")
}

func TestPasswordChangeNeedsTheCurrentPassword(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	user := testutils.SeedAccount(t, tdb.DB(), 1000001, "N0CALL", "n0call")
	jar := testutils.Login(t, router, "n0call")
	other := testutils.Login(t, router, "n0call")

	for _, tc := range []struct {
		current string
		want    string
	}{
		{"", "Enter your current password."},
		{"not-my-password", "The current password is wrong."},
	} {
		w := testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": "a-new-password", "current_password": tc.current})
		assert.Equal(t, http.StatusBadRequest, w.Code, tc.want)
		assert.JSONEq(t, fmt.Sprintf(`{"error":%q}`, tc.want), w.Body.String())
	}
	_, w, _ := testutils.LoginUser(t, router, apimodels.AuthLogin{Username: "n0call", Password: "a-new-password"})
	assert.Equal(t, http.StatusUnauthorized, w.Code, "a refused change must not set the password")
	assert.Equal(t, http.StatusOK, testutils.Do(t, router, other, http.MethodGet, "/api/v1/users/me", nil).Code, "a refused change must not end sessions")

	w = testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": "a-new-password", "current_password": testutils.TestPassword})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me", nil).Code, "the session that changed the password stays")
	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, other, http.MethodGet, "/api/v1/users/me", nil).Code, "the other sessions end")
	_, w, _ = testutils.LoginUser(t, router, apimodels.AuthLogin{Username: "n0call", Password: "a-new-password"})
	assert.Equal(t, http.StatusOK, w.Code)
}

func mustChange(t *testing.T, router *gin.Engine, jar testutils.CookieJar) bool {
	t.Helper()
	w := testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var me struct {
		MustChangePassword *bool `json:"must_change_password"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &me))
	require.NotNil(t, me.MustChangePassword, "GET /users/me tells whether the password must change")
	return *me.MustChangePassword
}

func loginWith(t *testing.T, router *gin.Engine, username, password string) (testutils.CookieJar, int) {
	t.Helper()
	_, w, jar := testutils.LoginUser(t, router, apimodels.AuthLogin{Username: username, Password: password})
	return jar, w.Code
}

func TestAdminResetEndsEverySessionAndMarksTheAccount(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	user := testutils.SeedAccount(t, tdb.DB(), 1000001, "N0CALL", "n0call")
	first := testutils.Login(t, router, "n0call")
	second := testutils.Login(t, router, "n0call")
	assert.False(t, mustChange(t, router, first))
	admin := testutils.Login(t, router, "Admin")

	w := testutils.Do(t, router, admin, http.MethodPatch, path(user.ID), map[string]string{"password": "Qm4tVx8rKp2Hw9Ns"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, first, http.MethodGet, "/api/v1/users/me", nil).Code)
	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, second, http.MethodGet, "/api/v1/users/me", nil).Code)
	assert.Equal(t, http.StatusOK, testutils.Do(t, router, admin, http.MethodGet, "/api/v1/users/me", nil).Code, "the admin stays logged in")
	assert.False(t, mustChange(t, router, admin))
	_, code := loginWith(t, router, "n0call", testutils.TestPassword)
	assert.Equal(t, http.StatusUnauthorized, code, "the old password is gone")

	jar, code := loginWith(t, router, "n0call", "Qm4tVx8rKp2Hw9Ns")
	require.Equal(t, http.StatusOK, code, "a user with a temporary password can log in")
	assert.True(t, mustChange(t, router, jar))

	w = testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": "my-own-password", "current_password": "Qm4tVx8rKp2Hw9Ns"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, mustChange(t, router, jar), "choosing their own password clears the mark")
}

func TestOnlyTheSystemAdminResetsAnAdmin(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	db := tdb.DB()
	testutils.SeedAccount(t, db, 1000001, "N0CALL", "admin1")
	other := testutils.SeedAccount(t, db, 1000002, "N1CALL", "admin2")
	user := testutils.SeedAccount(t, db, 1000003, "N2CALL", "user3")
	require.NoError(t, db.Model(&models.User{}).Where("id IN ?", []uint{1000001, 1000002}).Update("admin", true).Error)
	admin := testutils.Login(t, router, "admin1")

	w := testutils.Do(t, router, admin, http.MethodPatch, path(other.ID), map[string]string{"password": "Qm4tVx8rKp2Hw9Ns"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"Only the System Admin can reset the password of an admin."}`, w.Body.String())
	_, code := loginWith(t, router, "admin2", testutils.TestPassword)
	assert.Equal(t, http.StatusOK, code, "the refused reset must not change the password")

	w = testutils.Do(t, router, admin, http.MethodPatch, path(user.ID), map[string]string{"password": "Qm4tVx8rKp2Hw9Ns"})
	assert.Equal(t, http.StatusOK, w.Code, "an admin resets a user")

	super := testutils.Login(t, router, "Admin")
	w = testutils.Do(t, router, super, http.MethodPatch, path(other.ID), map[string]string{"password": "Qm4tVx8rKp2Hw9Ns"})
	assert.Equal(t, http.StatusOK, w.Code, "the System Admin resets an admin")
}
