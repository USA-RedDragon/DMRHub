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

package repeaters_test

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

func TestPOSTRepeaterRequiresLogin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/repeaters", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestPOSTRepeaterUnauthenticatedSingleResponse is a regression test ensuring
// that an unauthenticated POST to /api/v1/repeaters returns exactly one JSON
// response. Previously, missing `return` statements after error responses in
// the auth checks caused the handler to write multiple JSON responses and
// continue execution with a zero-value user ID.
func TestPOSTRepeaterUnauthenticatedSingleResponse(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Send a valid-looking JSON body so that if the handler falls through
	// past the auth check, it would attempt to process the request.
	body := map[string]interface{}{
		"radio_id": 123456,
	}
	jsonBytes, err := json.Marshal(body)
	assert.NoError(t, err)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/repeaters", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Verify the response body is valid JSON (a single object, not multiple
	// concatenated objects which would happen with double-writes).
	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err, "Response body should be a single valid JSON object, got: %s", w.Body.String())
	assert.Contains(t, resp, "error")
}

func TestPOSTRepeaterMMDVM(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// Use a hotspot-style ID (user DMR ID + 2 digits) to bypass repeaterdb validation
	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186801,
	}

	resp, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Repeater created", resp.Message)
	assert.NotEmpty(t, resp.Password)
	assert.Empty(t, resp.Error)

	// Verify the repeater was created correctly
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "mmdvm", rpt.Type)
	assert.True(t, rpt.Hotspot)
	assert.Equal(t, repeaterPost.RadioID, rpt.ID)
}

func TestPOSTRepeaterIPSC(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186802,
		Type:    "ipsc",
	}

	resp, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Repeater created", resp.Message)
	assert.NotEmpty(t, resp.Password)
	assert.Len(t, resp.Password, 40, "IPSC password should be 40-char hex string")
	assert.Empty(t, resp.Error)

	// Verify the repeater was created correctly
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ipsc", rpt.Type)
	assert.True(t, rpt.Hotspot)
}

func TestPATCHRepeaterPreservesType(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// Create an IPSC repeater
	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186803,
		Type:    "ipsc",
	}

	resp, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Error)

	// PATCH simplex_repeater
	simplexTrue := true
	patchResp, w := testutils.PatchRepeater(t, router, repeaterPost.RadioID, userJar, apimodels.RepeaterPatch{
		SimplexRepeater: &simplexTrue,
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Repeater updated", patchResp.Message)

	// GET the repeater and verify type is still "ipsc"
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ipsc", rpt.Type, "PATCH should not change repeater type")
	assert.True(t, rpt.SimplexRepeater)
}

func TestPATCHRepeaterPreservesPassword(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186804,
		Type:    "ipsc",
	}

	createResp, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)
	originalPassword := createResp.Password
	assert.NotEmpty(t, originalPassword)

	// PATCH the repeater
	simplexTrue := true
	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, userJar, apimodels.RepeaterPatch{
		SimplexRepeater: &simplexTrue,
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify password hasn't changed via direct DB query (password is json:"-")
	var dbRepeater models.Repeater
	err = tdb.DB().First(&dbRepeater, repeaterPost.RadioID).Error
	require.NoError(t, err)
	assert.Equal(t, originalPassword, dbRepeater.Password, "PATCH should not change repeater password")
}

func TestPATCHRepeaterPreservesHotspot(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// Create a hotspot repeater (hotspot-style ID)
	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186805,
	}

	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify it's a hotspot
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, rpt.Hotspot)

	// PATCH it
	simplexTrue := true
	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, userJar, apimodels.RepeaterPatch{
		SimplexRepeater: &simplexTrue,
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify hotspot is still true
	rpt, w = testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, rpt.Hotspot, "PATCH should not change hotspot status")
}

func TestPATCHRepeaterPreservesOwner(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186806,
		Type:    "ipsc",
	}

	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)

	// Admin PATCHes the repeater
	_, _, adminJar := testutils.LoginAdmin(t, router)
	simplexTrue := true
	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, adminJar, apimodels.RepeaterPatch{
		SimplexRepeater: &simplexTrue,
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify owner is still the original user
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, user.DMRId, rpt.Owner.ID, "PATCH should not change repeater owner")
}

func TestPOSTRepeaterTalkgroupsPreservesType(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// Create an IPSC repeater
	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186807,
		Type:    "ipsc",
	}
	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)

	// Create a talkgroup (as admin)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	tgResp, w := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		ID:          50001,
		Name:        "Test TG",
		Description: "Test talkgroup",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, tgResp.Error)

	// POST talkgroups to the repeater
	tgPost := apimodels.RepeaterTalkgroupsPost{
		TS1StaticTalkgroups: []models.Talkgroup{{ID: 50001}},
	}
	resp, w := testutils.SetRepeaterTalkgroups(t, router, repeaterPost.RadioID, userJar, tgPost)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Repeater talkgroups updated", resp.Message)

	// GET the repeater and verify type is still "ipsc"
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ipsc", rpt.Type, "POST talkgroups should not change repeater type")
}

func TestPOSTRepeaterTalkgroupsPreservesPassword(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186808,
		Type:    "ipsc",
	}
	createResp, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)
	originalPassword := createResp.Password

	// Create a talkgroup (as admin)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	_, w = testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		ID:          50002,
		Name:        "Test TG 2",
		Description: "Test talkgroup 2",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// POST talkgroups to the repeater
	tgPost := apimodels.RepeaterTalkgroupsPost{
		TS1StaticTalkgroups: []models.Talkgroup{{ID: 50002}},
	}
	_, w = testutils.SetRepeaterTalkgroups(t, router, repeaterPost.RadioID, userJar, tgPost)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify password hasn't changed via direct DB query
	var dbRepeater models.Repeater
	err = tdb.DB().First(&dbRepeater, repeaterPost.RadioID).Error
	require.NoError(t, err)
	assert.Equal(t, originalPassword, dbRepeater.Password, "POST talkgroups should not change repeater password")
}

func TestPOSTRepeaterLinkPreservesType(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// Create an IPSC repeater
	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186809,
		Type:    "ipsc",
	}
	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)

	// Create a talkgroup (as admin)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	_, w = testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		ID:          50003,
		Name:        "Link Test TG",
		Description: "Link test talkgroup",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Link the talkgroup dynamically
	_, w = testutils.LinkRepeater(t, router, repeaterPost.RadioID, "dynamic", "1", 50003, userJar)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET and verify type
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ipsc", rpt.Type, "Link should not change repeater type")
}

func TestPOSTRepeaterUnlinkPreservesType(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	// Create an IPSC repeater
	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186810,
		Type:    "ipsc",
	}
	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)

	// Create a talkgroup (as admin)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	_, w = testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		ID:          50004,
		Name:        "Unlink Test TG",
		Description: "Unlink test talkgroup",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Link then unlink
	_, w = testutils.LinkRepeater(t, router, repeaterPost.RadioID, "dynamic", "2", 50004, userJar)
	assert.Equal(t, http.StatusOK, w.Code)

	unlinkResp, w := testutils.UnlinkRepeater(t, router, repeaterPost.RadioID, "dynamic", "2", 50004, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Timeslot unlinked", unlinkResp.Message)

	// GET and verify type
	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ipsc", rpt.Type, "Unlink should not change repeater type")
}

func TestDELETERepeater(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	}

	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	repeaterPost := apimodels.RepeaterPost{
		RadioID: 319186811,
	}
	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	assert.Equal(t, http.StatusOK, w.Code)

	// Delete the repeater
	delResp, w := testutils.DeleteRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Repeater deleted", delResp.Message)

	// Verify it's gone
	_, w = testutils.GetRepeater(t, router, repeaterPost.RadioID, userJar)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPATCHRepeaterNonexistent(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	simplexTrue := true
	resp, w := testutils.PatchRepeater(t, router, 9999999, adminJar, apimodels.RepeaterPatch{
		SimplexRepeater: &simplexTrue,
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "repeater does not exist", resp.Error)
}
