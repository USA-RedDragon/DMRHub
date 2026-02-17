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

package testutils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type PeerRulesResponse struct {
	Rules []models.PeerRule `json:"rules"`
	Error string            `json:"error"`
}

type PeerRuleCreateResponse struct {
	Message string          `json:"message"`
	Rule    models.PeerRule `json:"rule"`
	Error   string          `json:"error"`
}

type PeerCreateResponse struct {
	Message  string `json:"message"`
	Password string `json:"password"`
	Error    string `json:"error"`
}

func CreatePeer(t *testing.T, router *gin.Engine, jar CookieJar, peer apimodels.PeerPost) (PeerCreateResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(peer)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/peers", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp PeerCreateResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func GetPeer(t *testing.T, router *gin.Engine, id uint, jar CookieJar) (models.Peer, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/peers/%d", id), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp models.Peer
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func DeletePeer(t *testing.T, router *gin.Engine, id uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/peers/%d", id), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func PatchPeer(t *testing.T, router *gin.Engine, id uint, jar CookieJar, patch apimodels.PeerPatch) (models.Peer, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(patch)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/peers/%d", id), bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp models.Peer
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func GetPeerRules(t *testing.T, router *gin.Engine, peerID uint, jar CookieJar) (PeerRulesResponse, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/peers/%d/rules", peerID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp PeerRulesResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func CreatePeerRule(t *testing.T, router *gin.Engine, peerID uint, jar CookieJar, rule apimodels.PeerRulePost) (PeerRuleCreateResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(rule)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/peers/%d/rules", peerID), bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp PeerRuleCreateResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func DeletePeerRule(t *testing.T, router *gin.Engine, peerID uint, ruleID uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/peers/%d/rules/%d", peerID, ruleID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}
