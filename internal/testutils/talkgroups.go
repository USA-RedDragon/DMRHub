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

func CreateTalkgroup(t *testing.T, router *gin.Engine, jar CookieJar, tg apimodels.TalkgroupPost) (APIResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(tg)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/talkgroups", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func GetTalkgroup(t *testing.T, router *gin.Engine, id uint, jar CookieJar) (models.Talkgroup, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/talkgroups/%d", id), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp models.Talkgroup
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func PatchTalkgroup(t *testing.T, router *gin.Engine, id uint, jar CookieJar, patch apimodels.TalkgroupPatch) (APIResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(patch)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/talkgroups/%d", id), bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	return resp, w
}

func DeleteTalkgroup(t *testing.T, router *gin.Engine, id uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/talkgroups/%d", id), nil)
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

func SetTalkgroupAdmins(t *testing.T, router *gin.Engine, id uint, jar CookieJar, action apimodels.TalkgroupAdminAction) (APIResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(action)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/talkgroups/%d/admins", id), bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

func SetTalkgroupNCOs(t *testing.T, router *gin.Engine, id uint, jar CookieJar, action apimodels.TalkgroupAdminAction) (APIResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(action)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/talkgroups/%d/ncos", id), bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}
