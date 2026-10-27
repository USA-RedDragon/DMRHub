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
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const msgBadInvite = "This invite does not work. Ask an admin for a new one."

func TestRegistrationModes(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
				be.Migrated().Setup(t, c)
				testutils.SetupReady(c)
				c.HTTP.CanonicalHost = "https://hub.example.com/"
			})
			require.NoError(t, err)
			defer tdb.CloseDB()
			admin := testutils.Login(t, router, "Admin")
			reg := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "invited", Password: "password"}

			testutils.SaveSetting(t, router, admin, "registration", "closed")
			resp, w := testutils.RegisterUser(t, router, reg)
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Equal(t, "Registration is closed", resp.Error)
			w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodGet, "/api/v1/network/features", nil)
			assert.Contains(t, w.Body.String(), `"registration":"closed"`)

			testutils.SaveSetting(t, router, admin, "registration", "invite")
			resp, w = testutils.RegisterUser(t, router, reg)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, msgBadInvite, resp.Error)
			reg.Invite = "0123456789abcdef0123456789abcdef"
			resp, w = testutils.RegisterUser(t, router, reg)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, msgBadInvite, resp.Error)

			assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, "/api/v1/admin/invites", nil).Code)
			w = testutils.Do(t, router, admin, http.MethodPost, "/api/v1/admin/invites", nil)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			var made apimodels.NewInviteCode
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &made))
			assert.Len(t, made.Code, 32)
			assert.Equal(t, "https://hub.example.com/register#invite="+made.Code, made.Link)
			assert.Equal(t, "XXXXXX", made.CreatedByCallsign)
			assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), made.ExpiresAt, time.Minute)

			reg.Invite = made.Code
			resp, w = testutils.RegisterUser(t, router, reg)
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			assert.Equal(t, "Your account is ready. You can log in now.", resp.Message, "an invite approves the user")
			testutils.Login(t, router, "invited")

			reg2 := apimodels.UserRegistration{DMRId: 3191877, Callsign: "KQ4APS", Username: "second", Password: "password", Invite: made.Code}
			resp, w = testutils.RegisterUser(t, router, reg2)
			assert.Equal(t, http.StatusBadRequest, w.Code, "an invite works once")
			assert.Equal(t, msgBadInvite, resp.Error)

			w = testutils.Do(t, router, admin, http.MethodGet, "/api/v1/admin/invites", nil)
			require.Equal(t, http.StatusOK, w.Code)
			assert.NotContains(t, w.Body.String(), made.Code, "the list never shows a code")
			var list apimodels.InviteList
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
			require.Len(t, list.Invites, 1)
			require.NotNil(t, list.Invites[0].UsedBy)
			assert.Equal(t, uint(3191868), *list.Invites[0].UsedBy)
			assert.Equal(t, "KI5VMF", list.Invites[0].UsedByCallsign)
			assert.Equal(t, http.StatusNotFound, testutils.Do(t, router, admin, http.MethodDelete, "/api/v1/admin/invites/"+itoa(made.ID), nil).Code, "a used invite stays")

			w = testutils.Do(t, router, admin, http.MethodPost, "/api/v1/admin/invites", nil)
			require.Equal(t, http.StatusCreated, w.Code)
			var old apimodels.NewInviteCode
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &old))
			require.NoError(t, tdb.DB().Model(&models.Invite{}).Where("id = ?", old.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			reg2.Invite = old.Code
			resp, w = testutils.RegisterUser(t, router, reg2)
			assert.Equal(t, http.StatusBadRequest, w.Code, "an expired invite does not work")
			assert.Equal(t, msgBadInvite, resp.Error)
			assert.Equal(t, http.StatusOK, testutils.Do(t, router, admin, http.MethodDelete, "/api/v1/admin/invites/"+itoa(old.ID), nil).Code)

			testutils.SaveSetting(t, router, admin, "registration", "open")
			reg2.Invite = ""
			resp, w = testutils.RegisterUser(t, router, reg2)
			assert.Equal(t, http.StatusOK, w.Code, resp.Error)
			assert.True(t, strings.HasPrefix(resp.Message, "User created"), "open registration waits for approval")
		})
	}
}

func itoa(n uint) string {
	return strconv.FormatUint(uint64(n), 10)
}
