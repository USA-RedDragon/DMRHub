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
	"crypto/sha1" //#nosec G505 -- HIBP range queries take SHA-1
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	dmrhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const breachedPassword = "breached-password"

type fakeHIBP struct{}

func (fakeHIBP) RoundTrip(r *http.Request) (*http.Response, error) {
	sum := sha1.Sum([]byte(breachedPassword)) //#nosec G401 -- HIBP range queries take SHA-1
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	body := "0000000000000000000000000000000000A:1\r\n"
	if strings.HasSuffix(r.URL.Path, "/"+hash[:5]) {
		body += hash[5:] + ":42\r\n"
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func hibpRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) {
		c.HIBPAPIKey = "test-key"
		c.AutoApprove = true
	}, dmrhttp.WithHIBPClient(&http.Client{Transport: fakeHIBP{}}))
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	return router, tdb.DB()
}

func path(id uint) string {
	return fmt.Sprintf("/api/v1/users/%d", id)
}

func TestRegisterRefusesABreachedPassword(t *testing.T) {
	t.Parallel()
	router, _ := hibpRouter(t)

	resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "breached", Password: breachedPassword})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "Password has been reported in a data breach. Please use another one", resp.Error)

	resp, w = testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "fine", Password: "a-fine-password"})
	assert.Equal(t, http.StatusOK, w.Code, resp.Error)
}

func TestPasswordChangeRefusesABreachedPassword(t *testing.T) {
	t.Parallel()
	router, db := hibpRouter(t)
	user := testutils.SeedAccount(t, db, 1000001, "N0CALL", "n0call")
	jar := testutils.Login(t, router, "n0call")

	w := testutils.Do(t, router, jar, http.MethodPatch, path(user.ID), map[string]string{"password": breachedPassword, "current_password": testutils.TestPassword})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Password has been reported in a data breach. Please use another one")
	testutils.Login(t, router, "n0call")

	admin := testutils.Login(t, router, "Admin")
	w = testutils.Do(t, router, admin, http.MethodPatch, path(user.ID), map[string]string{"password": breachedPassword})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	testutils.Login(t, router, "n0call")
}
