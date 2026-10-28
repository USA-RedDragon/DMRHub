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

package auth_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginBackoffRefusesAnAccountAfterTenFailures(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			for _, on := range []bool{false, true} {
				router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
					be.Migrated().Setup(t, c)
					c.LoginBackoff = on
				})
				require.NoError(t, err)
				db := tdb.DB()
				testutils.SeedAccount(t, db, 1000001, "N0CALL", "jdoe")

				login := func(user, password string) (string, int) {
					resp, w, _ := testutils.LoginUser(t, router, apimodels.AuthLogin{Username: user, Password: password})
					return resp.Error, w.Code
				}
				for range models.LoginFailureLimit - 1 {
					_, code := login("jdoe", "wrong-password")
					require.Equal(t, http.StatusUnauthorized, code)
				}
				_, code := login("jdoe", testutils.TestPassword)
				require.Equal(t, http.StatusOK, code, "a login clears the count")
				for range models.LoginFailureLimit {
					_, code = login("n0call", "wrong-password")
					require.Equal(t, http.StatusUnauthorized, code)
				}
				msg, code := login("JDOE", testutils.TestPassword)
				if !on {
					assert.Equal(t, http.StatusOK, code, "no backoff when it is off")
					tdb.CloseDB()
					continue
				}
				assert.Equal(t, http.StatusTooManyRequests, code, "the callsign and the username are one account")
				assert.Equal(t, "Too many failed logins. Try again in 15 minutes.", msg)
				testutils.SeedAccount(t, db, 1000002, "N1CALL", "other")
				_, code = login("other", testutils.TestPassword)
				assert.Equal(t, http.StatusOK, code, "other accounts still log in")

				for range models.LoginFailureLimit {
					_, code = login("nobody", "wrong-password")
					require.Equal(t, http.StatusUnauthorized, code)
				}
				_, code = login("NoBody", "wrong-password")
				assert.Equal(t, http.StatusTooManyRequests, code, "a name that no user has answers alike")

				require.NoError(t, db.Model(&models.LoginFailure{}).Where("id > ?", 0).Update("at", time.Now().Add(-models.LoginFailureWindow-time.Minute)).Error)
				_, code = login("jdoe", testutils.TestPassword)
				assert.Equal(t, http.StatusOK, code, "the failures stop counting after 15 minutes")
				tdb.CloseDB()
			}
		})
	}
}

func TestLoginBackoffIsOnByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	assert.True(t, cfg.LoginBackoff)
}
