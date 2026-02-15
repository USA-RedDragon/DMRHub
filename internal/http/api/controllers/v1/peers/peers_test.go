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

package peers_test

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

func TestGETPeersRequiresAdmin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/peers", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGETPeersAsAdmin(t *testing.T) {
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

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/peers", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGETMyPeersRequiresLogin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/peers/my", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGETMyPeersAuthenticated(t *testing.T) {
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

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/peers/my", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPOSTPeerRequiresAdmin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/peers", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestPOSTPeerUnauthenticatedSingleResponse is a regression test ensuring
// that an unauthenticated POST to /api/v1/peers returns exactly one JSON
// response. Previously, missing `return` statements after error responses in
// the auth checks caused the handler to write multiple JSON responses and
// continue execution with a zero-value user ID.
func TestPOSTPeerUnauthenticatedSingleResponse(t *testing.T) {
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
		"id":      999999,
		"egress":  true,
		"ingress": true,
	}
	jsonBytes, err := json.Marshal(body)
	assert.NoError(t, err)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/peers", bytes.NewBuffer(jsonBytes))
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

func TestPOSTPeer(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Look up admin user ID
	var adminUser models.User
	err = tdb.DB().Where("username = ?", "Admin").First(&adminUser).Error
	require.NoError(t, err)

	peer := apimodels.PeerPost{
		ID:      100001,
		OwnerID: adminUser.ID,
		Ingress: true,
		Egress:  true,
	}

	resp, w := testutils.CreatePeer(t, router, adminJar, peer)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Peer created", resp.Message)
	assert.NotEmpty(t, resp.Password)
	assert.Empty(t, resp.Error)

	// Verify the peer via GET
	peerResp, w := testutils.GetPeer(t, router, peer.ID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, peer.ID, peerResp.ID)
	assert.True(t, peerResp.Ingress)
	assert.True(t, peerResp.Egress)
	assert.Equal(t, adminUser.ID, peerResp.Owner.ID)
}

func TestDELETEPeer(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Look up admin user ID
	var adminUser models.User
	err = tdb.DB().Where("username = ?", "Admin").First(&adminUser).Error
	require.NoError(t, err)

	peer := apimodels.PeerPost{
		ID:      100002,
		OwnerID: adminUser.ID,
		Ingress: true,
		Egress:  false,
	}

	_, w := testutils.CreatePeer(t, router, adminJar, peer)
	assert.Equal(t, http.StatusOK, w.Code)

	// Delete the peer
	delResp, w := testutils.DeletePeer(t, router, peer.ID, adminJar)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Peer deleted", delResp.Message)

	// Verify it's gone
	_, w = testutils.GetPeer(t, router, peer.ID, adminJar)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestPOSTPeerDuplicate(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Look up admin user ID
	var adminUser models.User
	err = tdb.DB().Where("username = ?", "Admin").First(&adminUser).Error
	require.NoError(t, err)

	peer := apimodels.PeerPost{
		ID:      100003,
		OwnerID: adminUser.ID,
		Ingress: true,
		Egress:  true,
	}

	resp, w := testutils.CreatePeer(t, router, adminJar, peer)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Peer created", resp.Message)

	// Try to create same peer ID again
	resp2, w := testutils.CreatePeer(t, router, adminJar, peer)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "Peer ID already exists", resp2.Error)
}
