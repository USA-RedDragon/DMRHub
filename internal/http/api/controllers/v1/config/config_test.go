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

package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTimeout = 1 * time.Minute

func serve(t *testing.T, router *gin.Engine, method, path string, body []byte, jar *testutils.CookieJar) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	var r io.Reader
	if body != nil {
		r = bytes.NewBuffer(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, r)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if jar != nil {
		for _, cookie := range jar.Cookies() {
			req.AddCookie(&cookie)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func adminRouter(t *testing.T) (*gin.Engine, *testutils.CookieJar) {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	_, w, jar := testutils.LoginAdmin(t, router)
	require.Equal(t, http.StatusOK, w.Code)
	return router, &jar
}

func TestConfigRoutesRequireAdmin(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, userJar := testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "testuser",
		Password: "password",
	})

	routes := []struct {
		method, path string
		body         []byte
	}{
		{http.MethodGet, "/api/v1/config", nil},
		{http.MethodPut, "/api/v1/config", []byte("{}")},
		{http.MethodGet, "/api/v1/config/validate", nil},
		{http.MethodPost, "/api/v1/config/validate", []byte("{}")},
		{http.MethodGet, "/api/v1/config/transcoding", nil},
	}
	for _, r := range routes {
		w := serve(t, router, r.method, r.path, r.body, nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s without a session", r.method, r.path)
		w = serve(t, router, r.method, r.path, r.body, &userJar)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s %s as a user", r.method, r.path)
	}
}

func TestGETConfig(t *testing.T) {
	t.Parallel()
	router, jar := adminRouter(t)

	w := serve(t, router, http.MethodGet, "/api/v1/config", nil, jar)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestGETConfigValidate(t *testing.T) {
	t.Parallel()
	router, jar := adminRouter(t)

	w := serve(t, router, http.MethodGet, "/api/v1/config/validate", nil, jar)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	_, hasValid := result["valid"]
	assert.True(t, hasValid)
	_, hasErrors := result["errors"]
	assert.True(t, hasErrors)
}

func TestPOSTConfigValidateEmptyBody(t *testing.T) {
	t.Parallel()
	router, jar := adminRouter(t)

	w := serve(t, router, http.MethodPost, "/api/v1/config/validate", []byte("{}"), jar)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	_, hasValid := result["valid"]
	assert.True(t, hasValid)
}

func TestPOSTConfigValidateInvalidJSON(t *testing.T) {
	t.Parallel()
	router, jar := adminRouter(t)

	w := serve(t, router, http.MethodPost, "/api/v1/config/validate", []byte("not json"), jar)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPUTConfigInvalidJSON(t *testing.T) {
	t.Parallel()
	router, jar := adminRouter(t)

	w := serve(t, router, http.MethodPut, "/api/v1/config", []byte("not json"), jar)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
