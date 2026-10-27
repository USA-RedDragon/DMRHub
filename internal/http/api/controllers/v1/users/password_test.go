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
	"time"

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

func TestATemporaryPasswordOpensOnlyThePasswordChange(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	user := testutils.SeedAccount(t, tdb.DB(), 1000001, "N0CALL", "n0call")
	testutils.SeedAccount(t, tdb.DB(), 1000002, "N1CALL", "n1call")
	admin := testutils.Login(t, router, "Admin")
	w := testutils.Do(t, router, admin, http.MethodPatch, path(user.ID), map[string]string{"password": "Qm4tVx8rKp2Hw9Ns"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	jar, code := loginWith(t, router, "n0call", "Qm4tVx8rKp2Hw9Ns")
	require.Equal(t, http.StatusOK, code)

	closed := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/me/settings"},
		{http.MethodGet, "/api/v1/users/me/radio-ids"},
		{http.MethodGet, "/api/v1/repeaters/my"},
		{http.MethodGet, "/api/v1/messages/inbox"},
		{http.MethodGet, "/api/v1/lastheard"},
		{http.MethodGet, "/api/v1/oauth/apps"},
		{http.MethodGet, "/ws/calls"},
		{http.MethodPatch, path(1000002)},
	}
	for _, r := range closed {
		w := testutils.Do(t, router, jar, r.method, r.path, nil)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s %s", r.method, r.path)
		assert.JSONEq(t, `{"error":"An admin set a temporary password for you. Choose your own password in Settings first.","must_change_password":true}`, w.Body.String(), "%s %s", r.method, r.path)
	}
	for _, p := range []string{"/api/v1/users/me", "/api/v1/network/name", "/api/v1/network/features", "/api/v1/version"} {
		assert.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodGet, p, nil).Code, p)
	}
	assert.Equal(t, http.StatusOK, testutils.Do(t, router, admin, http.MethodGet, "/api/v1/users/me/settings", nil).Code, "other users are not affected")

	w = testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": "my-own-password", "current_password": "Qm4tVx8rKp2Hw9Ns"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for _, p := range []string{"/api/v1/users/me/settings", "/api/v1/repeaters/my", "/api/v1/lastheard"} {
		assert.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodGet, p, nil).Code, p)
	}
	assert.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodPost, "/api/v1/auth/logout", nil).Code)
}

func TestATemporaryPasswordMustBeReplaced(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	user := testutils.SeedAccount(t, tdb.DB(), 1000001, "N0CALL", "n0call")
	admin := testutils.Login(t, router, "Admin")
	w := testutils.Do(t, router, admin, http.MethodPatch, path(user.ID), map[string]string{"password": "Qm4tVx8rKp2Hw9Ns"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	jar, code := loginWith(t, router, "n0call", "Qm4tVx8rKp2Hw9Ns")
	require.Equal(t, http.StatusOK, code)

	w = testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"username": "someone-else"})
	assert.Equal(t, http.StatusForbidden, w.Code, "only the password change is open")
	assert.JSONEq(t, `{"error":"An admin set a temporary password for you. Choose your own password in Settings first.","must_change_password":true}`, w.Body.String())

	w = testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": "Qm4tVx8rKp2Hw9Ns", "current_password": "Qm4tVx8rKp2Hw9Ns"})
	assert.Equal(t, http.StatusBadRequest, w.Code, "the admin knows the temporary password")
	assert.JSONEq(t, `{"error":"The new password must not be the current one."}`, w.Body.String())
	assert.True(t, mustChange(t, router, jar))

	var stored models.User
	require.NoError(t, tdb.DB().First(&stored, user.ID).Error)
	assert.Equal(t, "n0call", stored.Username)
}

func TestAdminEmailsShowTheirLineBreaks(t *testing.T) {
	t.Parallel()
	var mails <-chan string
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) {
		mails = testutils.StartSMTPSink(t, c)
		c.HTTP.CanonicalHost = "https://hub.example.com/"
		c.NetworkName = "Test Hub"
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	require.NoError(t, tdb.DB().Exec("UPDATE users SET email = ?, email_verified = ? WHERE username = ?", "admin@example.com", true, "Admin").Error)

	resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "newuser", Password: "a-fine-password"})
	require.Equal(t, http.StatusOK, w.Code, resp.Error)
	mail := <-mails
	assert.Contains(t, mail, "Content-Type: text/plain")
	assert.Contains(t, mail, "\r\n\r\nA new user has registered.\r\n\r\nUsername: newuser\r\nCallsign: KI5VMF\r\nDMR ID: 3191868\r\n\r\nTo approve them, open https://hub.example.com/admin/users/approval\r\n")
	assert.NotContains(t, mail, "&lt;")
	assert.NotContains(t, mail, "<br>")
}

func TestAdminEmailsGoOnlyToAVerifiedAddress(t *testing.T) {
	t.Parallel()
	var mails <-chan string
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) {
		mails = testutils.StartSMTPSink(t, c)
		c.HTTP.CanonicalHost = "https://hub.example.com/"
		c.NetworkName = "Test Hub"
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	require.NoError(t, tdb.DB().Exec("UPDATE users SET email = ?, email_verified = ? WHERE username = ?", "admin@example.com", false, "Admin").Error)

	resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "newuser", Password: "a-fine-password"})
	require.Equal(t, http.StatusOK, w.Code, resp.Error)
	select {
	case mail := <-mails:
		t.Fatalf("an unverified admin address got a mail: %q", mail)
	case <-time.After(time.Second):
	}
}
