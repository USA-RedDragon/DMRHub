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

package http_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/sessionstore"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sessionCookieAge(t *testing.T, router *gin.Engine) (testutils.CookieJar, int) {
	t.Helper()
	jar := testutils.Login(t, router, "Admin")
	for _, c := range jar.Cookies() {
		if c.Name == sessionstore.CookieName {
			return jar, c.MaxAge
		}
	}
	require.FailNow(t, "no session cookie")
	return jar, 0
}

func TestSessionLengthIsASetupSetting(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
				be.Migrated().Setup(t, c)
				testutils.SetupReady(c)
			})
			require.NoError(t, err)
			defer tdb.CloseDB()

			old, age := sessionCookieAge(t, router)
			assert.Equal(t, 30*24*60*60, age, "30 days by default")

			testutils.SaveSetting(t, router, old, "http.session-length", time.Hour)
			fresh, age := sessionCookieAge(t, router)
			assert.Equal(t, 60*60, age)

			require.NoError(t, tdb.DB().Model(&models.Session{}).Where("id <> ?", "").Update("updated_at", time.Now().Add(-2*time.Hour)).Error)
			assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, old, http.MethodGet, "/api/v1/users/me", nil).Code, "a shorter length ends the older sessions")
			assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, fresh, http.MethodGet, "/api/v1/users/me", nil).Code)

			again, _ := sessionCookieAge(t, router)
			assert.Equal(t, http.StatusOK, testutils.Do(t, router, again, http.MethodGet, "/api/v1/users/me", nil).Code)
		})
	}
}
