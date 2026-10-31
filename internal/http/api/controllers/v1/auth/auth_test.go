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

package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/auth"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSysadminLogin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	resp, w, _ := testutils.LoginAdmin(t, router)

	assert.Equal(t, 200, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)
}

func TestLogout(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	resp, w, adminJar := testutils.LoginAdmin(t, router)

	assert.Equal(t, 200, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = testutils.LogoutUser(t, router, adminJar)

	assert.Equal(t, 200, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged out", resp.Message)
}

// TestLoginTimingOracle checks that a login for an account that does not
// exist takes the same time as a login with a wrong password, so that the
// response time does not tell an attacker which accounts exist. Both take
// the calibrated floor of the handler, measured in virtual time.
func TestLoginTimingOracle(t *testing.T) {
	t.Parallel()
	floor := auth.LoginFloor()
	require.Positive(t, floor)

	synctest.Test(t, func(t *testing.T) {
		router, tdb, err := testutils.CreateTestDBRouter()
		require.NoError(t, err)
		defer tdb.CloseDB()

		for _, login := range []apimodels.AuthLogin{
			{Username: "Admin", Password: "wrongpassword"},
			{Username: "nonexistentuser", Password: "wrongpassword"},
		} {
			start := time.Now()
			resp, w, _ := testutils.LoginUser(t, router, login)
			assert.Equal(t, floor, time.Since(start), "the login of %s takes the floor", login.Username)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, "Authentication failed", resp.Error)
		}
	})
}

// TestLogoutGETNotAllowed verifies that logout is only accessible via POST
// to prevent CSRF attacks via image tags or link prefetching.
func TestLogoutGETNotAllowed(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/logout", nil)
	assert.NoError(t, err)

	for _, cookie := range adminJar.Cookies() {
		req.AddCookie(&cookie)
	}

	router.ServeHTTP(w, req)

	// GET should be rejected (404 Not Found or 405 Method Not Allowed)
	assert.NotEqual(t, 200, w.Code, "Logout via GET should not be allowed")
}

func TestLoginTakesAUsernameOrACallsign(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	testutils.SeedAccount(t, tdb.DB(), 1000001, "N0CALL", "jdoe")
	testutils.SeedAccount(t, tdb.DB(), 1000002, "N1CALL", "n0call")

	for _, name := range []string{"jdoe", "N0CALL", "n0call"} {
		_, w, jar := testutils.LoginUser(t, router, apimodels.AuthLogin{Username: name, Password: testutils.TestPassword})
		require.Equal(t, http.StatusOK, w.Code, name)
		me := testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me", nil)
		want := "1000001"
		if name != "jdoe" {
			want = "1000002"
		}
		assert.Contains(t, me.Body.String(), `"id":`+want, "%s: a username in any case wins over a callsign", name)
	}
	_, w, _ := testutils.LoginUser(t, router, apimodels.AuthLogin{Username: "N0CALL", Password: "wrong"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
