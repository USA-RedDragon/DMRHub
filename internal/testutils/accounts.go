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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestPassword is the password of the users that SeedAccount makes.
const TestPassword = "password"

// SeedAccount makes an approved user with the password TestPassword, for
// the routers of CreateTestDBRouter and its variants.
func SeedAccount(t *testing.T, db *gorm.DB, id uint, callsign, username string) models.User {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	hash, err := utils.HashPassword(TestPassword, cfg.PasswordSalt)
	require.NoError(t, err)
	user := models.User{ID: id, Callsign: callsign, Username: username, Password: hash, Approved: true}
	require.NoError(t, db.Create(&user).Error)
	return user
}

// Login logs username in with TestPassword and returns the session.
func Login(t *testing.T, router *gin.Engine, username string) CookieJar {
	t.Helper()
	_, w, jar := LoginUser(t, router, apimodels.AuthLogin{Username: username, Password: TestPassword})
	require.Equal(t, http.StatusOK, w.Code, "login of %s", username)
	return jar
}

// Do sends a request with the cookies of jar and, when body is not nil,
// body as JSON.
func Do(t *testing.T, router *gin.Engine, jar CookieJar, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, path, r)
	require.NoError(t, err)
	for _, c := range jar.Cookies() {
		req.Header.Add("Cookie", c.String())
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// JSONKeys returns every object key in the JSON document b, at any depth.
func JSONKeys(t *testing.T, b []byte) map[string]bool {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(b, &v))
	keys := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				keys[k] = true
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return keys
}
