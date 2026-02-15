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
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

const testTimeout = 1 * time.Minute

func TestGETTalkgroupsRequiresLogin(t *testing.T) {
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

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGETTalkgroupsAuthenticated(t *testing.T) {
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

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/talkgroups", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
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
