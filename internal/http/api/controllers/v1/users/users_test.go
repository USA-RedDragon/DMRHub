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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

const testTimeout = 1 * time.Minute

func TestRegisterBadUser(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	// Test invalid user
	user := apimodels.UserRegistration{
		DMRId:    0,
		Callsign: "",
		Username: "",
		Password: "",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, "JSON data is invalid", resp.Error)
}

func TestRegisterBadData(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/users", bytes.NewBuffer([]byte("invalid json data")))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.Equal(t, "JSON data is invalid", resp.Error)
}

func TestRegisterBadDMRId(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    1,
		Callsign: "N0CALL",
		Username: "n0call",
		Password: "password",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, 400, w.Code)
	assert.Empty(t, resp.Message)
	assert.Equal(t, "DMR ID is not valid", resp.Error)
}

func TestRegisterBadCallsign(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "N0CALL",
		Username: "n0call",
		Password: "password",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, 400, w.Code)
	assert.Empty(t, resp.Message)
	assert.Equal(t, "Callsign does not match DMR ID", resp.Error)
}

func TestRegisterLowercaseCallsign(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "ki5vmf",
		Username: "username",
		Password: "password",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)
}

func TestRegisterUppercaseCallsign(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)
}

func TestRegisterDuplicateUsername(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)

	user = apimodels.UserRegistration{
		DMRId:    3140598,
		Callsign: "KP4DJT",
		Username: "username",
		Password: "password",
	}

	resp, w = testutils.RegisterUser(t, router, user)

	assert.Equal(t, 400, w.Code)
	assert.Empty(t, resp.Message)
	assert.Equal(t, "Username is already taken", resp.Error)
}

func TestRegisterDuplicateDMRID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)

	user = apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username2",
		Password: "password",
	}

	resp, w = testutils.RegisterUser(t, router, user)

	assert.Equal(t, 400, w.Code)
	assert.Empty(t, resp.Message)
	assert.Equal(t, "DMR ID is already registered", resp.Error)
}

func TestGetUsers(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	resp, w, jar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	userResp, w := testutils.ListUsers(t, router, jar)

	assert.Equal(t, http.StatusOK, w.Code)

	assert.Equal(t, 2, userResp.Total)
	for _, user := range userResp.Users {
		if user.ID != dmrconst.ParrotUser {
			assert.Equal(t, true, user.SuperAdmin)
			assert.Equal(t, "Admin", user.Username)
			assert.Equal(t, "XXXXXX", user.Callsign)
			assert.Equal(t, true, user.Admin)
			assert.Equal(t, true, user.Approved)
			assert.Equal(t, false, user.Suspended)
		} else {
			assert.Equal(t, dmrconst.ParrotUser, user.ID)
			assert.Equal(t, "", user.Username)
			assert.Equal(t, "Parrot", user.Callsign)
			assert.Equal(t, false, user.Admin)
			assert.Equal(t, true, user.Approved)
			assert.Equal(t, false, user.Suspended)
		}
	}
}

func TestDeleteUser(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "ki5vmf",
		Username: "username",
		Password: "password",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)

	resp, w, jar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = testutils.DeleteUser(t, router, user.DMRId, jar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User deleted", resp.Message)
}

func TestDeleteInvalidUser(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "ki5vmf",
		Username: "username",
		Password: "password",
	}

	resp, w := testutils.RegisterUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)

	resp, w, jar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = testutils.DeleteUser(t, router, user.DMRId+1, jar)

	assert.Equal(t, 400, w.Code)
	assert.Empty(t, resp.Message)
	assert.Equal(t, "User does not exist", resp.Error)
}

func TestApproveUser(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "ki5vmf",
		Username: "username",
		Password: "password",
	}

	resp, w, _ := testutils.CreateAndLoginUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)
}

func TestSuspendUser(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "ki5vmf",
		Username: "username",
		Password: "password",
	}

	resp, w, _ := testutils.CreateAndLoginUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w, jar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = testutils.SuspendUser(t, router, user.DMRId, jar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User suspended", resp.Message)

	resp, w, _ = testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: user.Username,
		Password: user.Password,
	})

	assert.Equal(t, 401, w.Code)
	assert.NotEmpty(t, w.Body.String())

	assert.Empty(t, resp.Message)
	assert.Equal(t, "User is suspended", resp.Error)
}

func TestUnsuspendUser(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "ki5vmf",
		Username: "username",
		Password: "password",
	}

	resp, w, _ := testutils.CreateAndLoginUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w, jar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = testutils.SuspendUser(t, router, user.DMRId, jar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User suspended", resp.Message)

	resp, w, _ = testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: user.Username,
		Password: user.Password,
	})

	assert.Equal(t, 401, w.Code)
	assert.Empty(t, resp.Message)
	assert.Equal(t, "User is suspended", resp.Error)

	resp, w = testutils.UnsuspendUser(t, router, user.DMRId, jar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User unsuspended", resp.Message)

	resp, w, _ = testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: user.Username,
		Password: user.Password,
	})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)
}

func TestGetUserByID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	resp, w, _ := testutils.CreateAndLoginUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w, jar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	userResp, w := testutils.GetUser(t, router, user.DMRId, jar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, user.DMRId, userResp.ID)
	assert.Equal(t, user.Callsign, userResp.Callsign)
	assert.Equal(t, user.Username, userResp.Username)
}

func TestGetUserByIDNotFound(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	resp, w, jar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	w = httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Request a user ID that does not exist
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users/9999999", nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	// The body must be a single valid JSON object with an error field.
	// Before the fix, two JSON objects were concatenated, so this unmarshal would fail.
	var errResp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &errResp)
	assert.NoError(t, err)
	assert.Equal(t, "User does not exist", errResp.Error)
	assert.Empty(t, errResp.Message)
}

func TestGetUserMe(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	resp, w, jar := testutils.CreateAndLoginUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	userResp, w := testutils.GetUserMe(t, router, jar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, user.DMRId, userResp.ID)
	assert.Equal(t, user.Callsign, userResp.Callsign)
	assert.Equal(t, user.Username, userResp.Username)
}

func TestPromoteUser(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	resp, w, userJar := testutils.CreateAndLoginUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w, adminJar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = testutils.PromoteUser(t, router, user.DMRId, adminJar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User promoted", resp.Message)

	userResp, w := testutils.GetUserMe(t, router, userJar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, user.DMRId, userResp.ID)
	assert.Equal(t, user.Callsign, userResp.Callsign)
	assert.Equal(t, user.Username, userResp.Username)
	assert.Equal(t, true, userResp.Admin)
}

func TestDemoteUser(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	resp, w, userJar := testutils.CreateAndLoginUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w, adminJar := testutils.LoginAdmin(t, router)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = testutils.PromoteUser(t, router, user.DMRId, adminJar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User promoted", resp.Message)

	userResp, w := testutils.GetUserMe(t, router, userJar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, user.DMRId, userResp.ID)
	assert.Equal(t, user.Callsign, userResp.Callsign)
	assert.Equal(t, user.Username, userResp.Username)
	assert.Equal(t, true, userResp.Admin)

	resp, w = testutils.DemoteUser(t, router, user.DMRId, adminJar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User demoted", resp.Message)

	userResp, w = testutils.GetUserMe(t, router, userJar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, user.DMRId, userResp.ID)
	assert.Equal(t, user.Callsign, userResp.Callsign)
	assert.Equal(t, user.Username, userResp.Username)
	assert.Equal(t, false, userResp.Admin)
}

func TestPATCHUserPreservesAdmin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Promote user to admin
	resp, w := testutils.PromoteUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User promoted", resp.Message)

	// PATCH the user's username (self-edit)
	patchResp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{
		Username: "newusername",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User updated", patchResp.Message)

	// Verify admin is still true
	userResp, w := testutils.GetUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, userResp.Admin, "PATCH username should not change admin status")
}

func TestPATCHUserPreservesSuperAdmin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Look up the admin user's ID
	var adminUser models.User
	err = tdb.DB().Where("username = ?", "Admin").First(&adminUser).Error
	assert.NoError(t, err)

	// The admin user is SuperAdmin. PATCH their username.
	patchResp, w := testutils.PatchUser(t, router, adminUser.ID, adminJar, apimodels.UserPatch{
		Username: "AdminRenamed",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User updated", patchResp.Message)

	// Verify superAdmin is still true via direct DB query (since we renamed the user)
	var updatedAdmin models.User
	err = tdb.DB().First(&updatedAdmin, adminUser.ID).Error
	assert.NoError(t, err)
	assert.True(t, updatedAdmin.SuperAdmin, "PATCH username should not change superAdmin status")
	assert.True(t, updatedAdmin.Admin, "PATCH username should not change admin status")
}

func TestPATCHUserPreservesApproved(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// User is approved now. PATCH their username.
	patchResp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{
		Username: "newusername",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User updated", patchResp.Message)

	// Verify approved is still true
	_, _, adminJar := testutils.LoginAdmin(t, router)
	userResp, w := testutils.GetUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, userResp.Approved, "PATCH username should not change approved status")
}

func TestPATCHUserPreservesSuspended(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, _ = testutils.CreateAndLoginUser(t, router, user)
	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Suspend the user
	resp, w := testutils.SuspendUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User suspended", resp.Message)

	// Admin PATCHes the user's username
	patchResp, w := testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{
		Username: "newusername",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User updated", patchResp.Message)

	// Verify suspended is still true
	userResp, w := testutils.GetUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, userResp.Suspended, "PATCH username should not change suspended status")
}

func TestPATCHUserPreservesPassword(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// PATCH only the username
	patchResp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{
		Username: "newusername",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User updated", patchResp.Message)

	// Verify old password still works
	loginResp, w, _ := testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: "newusername",
		Password: "password",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Logged in", loginResp.Message)
}

func TestPATCHUserChangesPassword(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// PATCH with a new password
	patchResp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{
		Password: "newpassword123",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User updated", patchResp.Message)

	// Verify new password works
	loginResp, w, _ := testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: user.Username,
		Password: "newpassword123",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Logged in", loginResp.Message)

	// Verify old password no longer works
	_, w, _ = testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: user.Username,
		Password: "password",
	})
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestSuspendUserPreservesAdmin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, _ = testutils.CreateAndLoginUser(t, router, user)
	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Suspend the user first (before promoting)
	resp, w := testutils.SuspendUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User suspended", resp.Message)

	// Now promote the user (while suspended)
	resp, w = testutils.PromoteUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User promoted", resp.Message)

	// Verify both admin and suspended are true
	userResp, w := testutils.GetUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, userResp.Admin, "Promote should preserve admin status")
	assert.True(t, userResp.Suspended, "Promote should not change suspended status")
}

func TestApproveUserPreservesCallsign(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	// Register user (not approved yet)
	regResp, w := testutils.RegisterUser(t, router, user)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User created, please wait for admin approval", regResp.Message)

	// Approve user as admin
	_, _, adminJar := testutils.LoginAdmin(t, router)
	resp, w := testutils.ApproveUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User approved", resp.Message)

	// Verify callsign is preserved
	userResp, w := testutils.GetUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, user.Callsign, userResp.Callsign, "Approve should not change callsign")
	assert.True(t, userResp.Approved)
}

func TestPromoteUserPreservesSuspended(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, _ = testutils.CreateAndLoginUser(t, router, user)
	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Suspend the user
	resp, w := testutils.SuspendUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User suspended", resp.Message)

	// Promote the user
	resp, w = testutils.PromoteUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User promoted", resp.Message)

	// Verify suspended is still true
	userResp, w := testutils.GetUser(t, router, user.DMRId, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, userResp.Suspended, "Promote should not change suspended status")
	assert.True(t, userResp.Admin)
}
