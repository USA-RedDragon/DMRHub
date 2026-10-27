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
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const msgShortPassword = "The password must have at least 8 characters."

func TestNewPasswordsNeedEightCharacters(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
				be.Migrated().Setup(t, c)
			})
			require.NoError(t, err)
			defer tdb.CloseDB()
			db := tdb.DB()

			resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "shorty", Password: "seven77"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, msgShortPassword, resp.Error)
			resp, w = testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "shorty", Password: "ééééééé"})
			assert.Equal(t, http.StatusBadRequest, w.Code, "7 characters in 14 bytes")
			assert.Equal(t, msgShortPassword, resp.Error)

			_, _, jar := testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "shorty", Password: "eight888"})
			resp, w = testutils.PatchUser(t, router, 3191868, jar, apimodels.UserPatch{Password: "seven77", CurrentPassword: "eight888"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, msgShortPassword, resp.Error)

			_, _, admin := testutils.LoginAdmin(t, router)
			resp, w = testutils.PatchUser(t, router, 3191868, admin, apimodels.UserPatch{Password: "seven77"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, msgShortPassword, resp.Error)

			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			hash, err := utils.HashPassword("abc", cfg.PasswordSalt)
			require.NoError(t, err)
			require.NoError(t, db.Create(&models.User{ID: 3191869, Callsign: "N0OLD", Username: "oldpass", Approved: true, Password: hash}).Error)
			_, w, _ = testutils.LoginUser(t, router, apimodels.AuthLogin{Username: "oldpass", Password: "abc"})
			assert.Equal(t, http.StatusOK, w.Code, "an old short password still logs in")
		})
	}
}
