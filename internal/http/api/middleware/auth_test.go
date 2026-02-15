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

package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

const testTimeout = 1 * time.Minute

func TestRequireLoginBlocksUnauthenticated(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/repeaters/my", nil)
	assert.NoError(t, err)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireLoginAllowsAuthenticated(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	_, _, jar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/repeaters/my", nil)
	assert.NoError(t, err)
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireAdminBlocksNonAdmin(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		Username: "testuser",
		Password: "testpassword123!",
		Callsign: "KI5VMF",
		DMRId:    3191868,
	}
	_, _, jar := testutils.CreateAndLoginUser(t, router, user)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// /api/v1/repeaters requires admin
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/repeaters", nil)
	assert.NoError(t, err)
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireAdminAllowsAdmin(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	_, _, jar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/repeaters", nil)
	assert.NoError(t, err)
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireSelfOrAdminBlocksOtherUser(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		Username: "selftest",
		Password: "testpassword123!",
		Callsign: "KI5VMF",
		DMRId:    3191868,
	}
	_, _, jar := testutils.CreateAndLoginUser(t, router, user)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Try to access another user's profile (admin user ID 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users/999999", nil)
	assert.NoError(t, err)
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireSelfOrAdminAllowsSelf(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		Username: "selftest2",
		Password: "testpassword123!",
		Callsign: "KI5VMF",
		DMRId:    3191868,
	}
	_, _, jar := testutils.CreateAndLoginUser(t, router, user)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Access own profile using DMR ID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users/3191868", nil)
	assert.NoError(t, err)
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSuspendedUserBlocked(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		Username: "suspendme",
		Password: "testpassword123!",
		Callsign: "KI5VMF",
		DMRId:    3191868,
	}
	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// Now suspend the user using admin
	_, _, adminJar := testutils.LoginAdmin(t, router)
	resp, w := testutils.SuspendUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User suspended", resp.Message)

	// Suspended user should be blocked from accessing login-required endpoints
	w = httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/talkgroups", nil)
	assert.NoError(t, err)
	for _, cookie := range userJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireSuperAdminBlocksRegularAdmin(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	// Create a regular user, promote to admin but not super admin
	user := apimodels.UserRegistration{
		Username: "regularadmin",
		Password: "testpassword123!",
		Callsign: "KI5VMF",
		DMRId:    3191868,
	}
	testutils.CreateAndLoginUser(t, router, user)

	// Login as super admin and promote user to admin
	_, _, superJar := testutils.LoginAdmin(t, router)
	_, w := testutils.PromoteUser(t, router, user.DMRId, superJar)
	assert.Equal(t, http.StatusOK, w.Code)

	// Login as the promoted user
	_, _, promotedJar := testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: "regularadmin",
		Password: "testpassword123!",
	})

	// SuperAdmin-only route: DELETE /api/v1/users/:id
	w = httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users/admins", nil)
	assert.NoError(t, err)
	for _, cookie := range promotedJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	// Promoted admin is not a super admin, so this should be blocked
	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireSuperAdminBlocksSuspendedSuperAdmin(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	// Login as the super admin (seeded Admin user)
	_, _, superJar := testutils.LoginAdmin(t, router)

	// Verify access works before suspension
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users/admins", nil)
	assert.NoError(t, err)
	for _, cookie := range superJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Directly suspend the super admin via DB
	db := tdb.DB()
	assert.NotNil(t, db)
	err = db.Model(&models.User{}).Where("username = ?", "Admin").Update("suspended", true).Error
	assert.NoError(t, err)

	// Suspended super admin should now be blocked
	w = httptest.NewRecorder()
	ctx2, cancel2 := context.WithTimeout(context.Background(), testTimeout)
	defer cancel2()

	req, err = http.NewRequestWithContext(ctx2, http.MethodGet, "/api/v1/users/admins", nil)
	assert.NoError(t, err)
	for _, cookie := range superJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireSuperAdminBlocksUnapprovedSuperAdmin(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	assert.NoError(t, err)
	defer tdb.CloseDB()

	// Login as the super admin (seeded Admin user)
	_, _, superJar := testutils.LoginAdmin(t, router)

	// Verify access works before unapproving
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users/admins", nil)
	assert.NoError(t, err)
	for _, cookie := range superJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Directly unapprove the super admin via DB
	db := tdb.DB()
	assert.NotNil(t, db)
	err = db.Model(&models.User{}).Where("username = ?", "Admin").Update("approved", false).Error
	assert.NoError(t, err)

	// Unapproved super admin should now be blocked
	w = httptest.NewRecorder()
	ctx2, cancel2 := context.WithTimeout(context.Background(), testTimeout)
	defer cancel2()

	req, err = http.NewRequestWithContext(ctx2, http.MethodGet, "/api/v1/users/admins", nil)
	assert.NoError(t, err)
	for _, cookie := range superJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
