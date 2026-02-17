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

package talkgroups_test

import (
	"bytes"
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
	"github.com/stretchr/testify/require"
)

const testTimeout = 1 * time.Minute

func TestGETTalkgroupsAuthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/talkgroups", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total      int                `json:"total"`
		Talkgroups []models.Talkgroup `json:"talkgroups"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	// Should have at least the parrot talkgroup (seeded)
	assert.GreaterOrEqual(t, result.Total, 1)
}

func TestGETTalkgroupByID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Talkgroup 9990 is the parrot talkgroup (seeded)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/talkgroups/9990", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var tg models.Talkgroup
	err = json.Unmarshal(w.Body.Bytes(), &tg)
	assert.NoError(t, err)
	assert.Equal(t, uint(9990), tg.ID)
	assert.Equal(t, "DMRHub Parrot", tg.Name)
}

// TestGETTalkgroupByIDReturnsAdminsAndNCOs is a regression test ensuring that
// GETTalkgroup returns the Admins and NCOs associations in the response. A
// previous bug caused GETTalkgroup to make two redundant DB queries, where the
// second (which populated the associations) had its error discarded. After the
// fix, FindTalkgroupByID is called once and already preloads Admins and NCOs.
func TestGETTalkgroupByIDReturnsAdminsAndNCOs(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Look up the admin user's actual ID from the database
	var adminUser models.User
	err = tdb.DB().Where("username = ?", "Admin").First(&adminUser).Error
	assert.NoError(t, err)

	// Create a talkgroup
	createBody := map[string]interface{}{
		"id":          uint(12345),
		"name":        "Test TG",
		"description": "Test talkgroup",
	}
	jsonBytes, err := json.Marshal(createBody)
	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/talkgroups", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Assign admin user as an admin of the talkgroup
	adminAction := map[string]interface{}{
		"user_ids": []uint{adminUser.ID},
	}
	jsonBytes, err = json.Marshal(adminAction)
	assert.NoError(t, err)

	w = httptest.NewRecorder()
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/talkgroups/12345/admins", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Also assign as NCO
	w = httptest.NewRecorder()
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/talkgroups/12345/ncos", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET the talkgroup and verify Admins and NCOs are populated
	w = httptest.NewRecorder()
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/talkgroups/12345", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var tg models.Talkgroup
	err = json.Unmarshal(w.Body.Bytes(), &tg)
	assert.NoError(t, err)
	assert.Equal(t, uint(12345), tg.ID)
	assert.Equal(t, "Test TG", tg.Name)
	assert.Len(t, tg.Admins, 1, "Admins should be populated in GET response")
	assert.Equal(t, "Admin", tg.Admins[0].Username)
	assert.Len(t, tg.NCOs, 1, "NCOs should be populated in GET response")
	assert.Equal(t, "Admin", tg.NCOs[0].Username)
}

func TestGETTalkgroupInvalidID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/talkgroups/notanumber", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGETMyTalkgroupsAuthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/talkgroups/my", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPOSTTalkgroup(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	tg := apimodels.TalkgroupPost{
		ID:          10001,
		Name:        "My Test TG",
		Description: "A test talkgroup",
	}

	resp, w := testutils.CreateTalkgroup(t, router, adminJar, tg)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)

	// Verify it appears in GET
	tgResp, w := testutils.GetTalkgroup(t, router, tg.ID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, tg.ID, tgResp.ID)
	assert.Equal(t, tg.Name, tgResp.Name)
	assert.Equal(t, tg.Description, tgResp.Description)
}

func TestPATCHTalkgroupPreservesName(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	tg := apimodels.TalkgroupPost{
		ID:          10002,
		Name:        "Original Name",
		Description: "Original description",
	}

	_, w := testutils.CreateTalkgroup(t, router, adminJar, tg)
	assert.Equal(t, http.StatusOK, w.Code)

	// PATCH only description
	_, w = testutils.PatchTalkgroup(t, router, tg.ID, adminJar, apimodels.TalkgroupPatch{
		Description: "Updated description",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify name unchanged
	tgResp, w := testutils.GetTalkgroup(t, router, tg.ID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Original Name", tgResp.Name, "PATCH description should not change name")
	assert.Equal(t, "Updated description", tgResp.Description)
}

func TestPATCHTalkgroupPreservesDescription(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	tg := apimodels.TalkgroupPost{
		ID:          10003,
		Name:        "Original Name",
		Description: "Original description",
	}

	_, w := testutils.CreateTalkgroup(t, router, adminJar, tg)
	assert.Equal(t, http.StatusOK, w.Code)

	// PATCH only name
	_, w = testutils.PatchTalkgroup(t, router, tg.ID, adminJar, apimodels.TalkgroupPatch{
		Name: "Updated Name",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify description unchanged
	tgResp, w := testutils.GetTalkgroup(t, router, tg.ID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Updated Name", tgResp.Name)
	assert.Equal(t, "Original description", tgResp.Description, "PATCH name should not change description")
}

func TestPOSTTalkgroupAdminsPreservesFields(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	tg := apimodels.TalkgroupPost{
		ID:          10004,
		Name:        "Admin Test TG",
		Description: "A talkgroup for admin tests",
	}

	_, w := testutils.CreateTalkgroup(t, router, adminJar, tg)
	assert.Equal(t, http.StatusOK, w.Code)

	// Look up the admin user's actual ID
	var adminUser models.User
	err = tdb.DB().Where("username = ?", "Admin").First(&adminUser).Error
	require.NoError(t, err)

	// Set admins
	resp, w := testutils.SetTalkgroupAdmins(t, router, tg.ID, adminJar, apimodels.TalkgroupAdminAction{
		UserIDs: []uint{adminUser.ID},
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)

	// Verify name and description unchanged
	tgResp, w := testutils.GetTalkgroup(t, router, tg.ID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Admin Test TG", tgResp.Name, "Setting admins should not change name")
	assert.Equal(t, "A talkgroup for admin tests", tgResp.Description, "Setting admins should not change description")
	assert.Len(t, tgResp.Admins, 1)
}

func TestPOSTTalkgroupNCOsPreservesFields(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	tg := apimodels.TalkgroupPost{
		ID:          10005,
		Name:        "NCO Test TG",
		Description: "A talkgroup for NCO tests",
	}

	_, w := testutils.CreateTalkgroup(t, router, adminJar, tg)
	assert.Equal(t, http.StatusOK, w.Code)

	// Look up the admin user's actual ID
	var adminUser models.User
	err = tdb.DB().Where("username = ?", "Admin").First(&adminUser).Error
	require.NoError(t, err)

	// Set NCOs
	resp, w := testutils.SetTalkgroupNCOs(t, router, tg.ID, adminJar, apimodels.TalkgroupAdminAction{
		UserIDs: []uint{adminUser.ID},
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)

	// Verify name and description unchanged
	tgResp, w := testutils.GetTalkgroup(t, router, tg.ID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "NCO Test TG", tgResp.Name, "Setting NCOs should not change name")
	assert.Equal(t, "A talkgroup for NCO tests", tgResp.Description, "Setting NCOs should not change description")
	assert.Len(t, tgResp.NCOs, 1)
}

func TestDELETETalkgroup(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	tg := apimodels.TalkgroupPost{
		ID:          10006,
		Name:        "Delete Test TG",
		Description: "A talkgroup to delete",
	}

	_, w := testutils.CreateTalkgroup(t, router, adminJar, tg)
	assert.Equal(t, http.StatusOK, w.Code)

	// Create a user and repeater, assign the talkgroup
	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}
	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186801,
	}
	_, w = testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)

	// Assign the talkgroup to the repeater
	_, w = testutils.SetRepeaterTalkgroups(t, router, repeaterPost.RadioID, userJar, apimodels.RepeaterTalkgroupsPost{
		TS1StaticTalkgroups: []models.Talkgroup{{ID: tg.ID}},
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Delete the talkgroup
	delResp, w := testutils.DeleteTalkgroup(t, router, tg.ID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Talkgroup deleted", delResp.Message)

	// Verify it's gone (controller returns 500 for record not found)
	_, w = testutils.GetTalkgroup(t, router, tg.ID, adminJar)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Verify repeater no longer references the talkgroup
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, rpt.TS1StaticTalkgroups, "Deleting a talkgroup should remove it from repeater associations")
}
