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
	"net/url"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPasswordResetsSignOutTheApps checks that an app that the user
// allowed loses its access when an admin or an emailed link resets the
// password, as after a takeover of the account.
func TestPasswordResetsSignOutTheApps(t *testing.T) {
	t.Parallel()
	reset := map[string]func(t *testing.T, e *oauthEnv){
		"by an admin": func(t *testing.T, e *oauthEnv) {
			admin := testutils.Login(t, e.router, "Admin")
			w := testutils.Do(t, e.router, admin, http.MethodPatch, "/api/v1/users/1000001", map[string]string{"password": "Qm4tVx8rKp2Hw9Ns"})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		},
		"by an emailed link": func(t *testing.T, e *oauthEnv) {
			token, err := models.CreatePasswordReset(e.tdb.DB(), 1000001, time.Now())
			require.NoError(t, err)
			w := testutils.Do(t, e.router, testutils.CookieJar{}, http.MethodPost, "/api/v1/auth/reset-password", map[string]string{"token": token, "password": "my-new-password"})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		},
	}
	for name, do := range reset {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newOAuthEnv(t)
			testutils.SeedAccount(t, e.tdb.DB(), 1000001, "N0CALL", "n0call")
			e.jar = testutils.Login(t, e.router, "n0call")
			tok := e.token(t, "talkgroups:read")
			require.Equal(t, http.StatusOK, e.bearer(t, http.MethodGet, "/api/v1/users/me", tok.AccessToken).Code)

			do(t, e)

			assert.Equal(t, http.StatusUnauthorized, e.bearer(t, http.MethodGet, "/api/v1/users/me", tok.AccessToken).Code, "the access token")
			w := e.form(t, "/api/v1/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}, "client_id": {appOrigin}})
			assert.Equal(t, http.StatusBadRequest, w.Code, "the refresh token")
		})
	}
}
