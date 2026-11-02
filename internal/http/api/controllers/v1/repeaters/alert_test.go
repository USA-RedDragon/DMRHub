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
	"encoding/json"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// TestASysopTurnsAnOfflineAlertOnAndOff turns the offline alert of a
// repeater on as its owner, with a verified address, changes it, and
// turns it off. A user without a verified address, a bad delay, a network
// without SMTP and another user are refused.
func TestASysopTurnsAnOfflineAlertOnAndOff(t *testing.T) {
	t.Parallel()
	for _, smtpOn := range []bool{true, false} {
		t.Run(map[bool]string{true: "smtp", false: "no-smtp"}[smtpOn], func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) { c.SMTP.Enabled = smtpOn })
				require.NoError(t, err)
				defer tdb.CloseDB()
				database := tdb.DB()
				owner := testutils.SeedAccount(t, database, 3002068, "N7CALL", "n7call")
				kevin := testutils.SeedAccount(t, database, 3002069, "N6CALL", "kevin")
				testutils.SeedAccount(t, database, 3002070, "N0CALL", "other")
				require.NoError(t, database.Exec("UPDATE users SET email = ?, email_verified = ? WHERE id = ?", "n7call@example.org", true, owner.ID).Error)
				require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: cosysopRepeater}, Type: models.RepeaterTypeMMDVM, Password: "password1234", OwnerID: owner.ID}).Error)
				now := time.Now()
				_, err = models.InviteCosysop(database, cosysopRepeater, kevin.ID, owner.ID, now)
				require.NoError(t, err)
				_, err = models.AnswerCosysopInvite(database, cosysopRepeater, kevin.ID, true, now)
				require.NoError(t, err)
				ownerJar := testutils.Login(t, router, "n7call")
				const path = "/api/v1/repeaters/300206801/alert"
				get := func(jar testutils.CookieJar) apimodels.OfflineAlert {
					t.Helper()
					w := testutils.Do(t, router, jar, http.MethodGet, path, nil)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					var out apimodels.OfflineAlert
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
					return out
				}

				a := get(ownerJar)
				require.Equal(t, apimodels.OfflineAlert{SMTP: smtpOn, EmailVerified: true, Email: "n***@example.org", DelayMinutes: 10, Delays: []uint{5, 10, 30, 60}}, a)

				w := testutils.Do(t, router, ownerJar, http.MethodPut, path, apimodels.OfflineAlertPut{Enabled: true, DelayMinutes: 30, Back: true})
				if !smtpOn {
					require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
					return
				}
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				a = get(ownerJar)
				require.True(t, a.Enabled)
				require.Equal(t, uint(30), a.DelayMinutes)
				require.True(t, a.Back)

				w = testutils.Do(t, router, ownerJar, http.MethodPut, path, apimodels.OfflineAlertPut{Enabled: true, DelayMinutes: 7})
				require.Equal(t, http.StatusBadRequest, w.Code)
				kevinJar := testutils.Login(t, router, "kevin")
				require.False(t, get(kevinJar).EmailVerified)
				w = testutils.Do(t, router, kevinJar, http.MethodPut, path, apimodels.OfflineAlertPut{Enabled: true, DelayMinutes: 5})
				require.Equal(t, http.StatusConflict, w.Code, "a co-sysop needs a verified address")
				w = testutils.Do(t, router, testutils.Login(t, router, "other"), http.MethodGet, path, nil)
				require.Equal(t, http.StatusForbidden, w.Code)

				w = testutils.Do(t, router, ownerJar, http.MethodPut, path, apimodels.OfflineAlertPut{Enabled: false})
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.False(t, get(ownerJar).Enabled)
			})
		})
	}
}
