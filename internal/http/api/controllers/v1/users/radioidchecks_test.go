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
	"fmt"
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func checksRouter(t *testing.T, be testutils.Backend, checks config.RadioIDChecks) (*gin.Engine, *gorm.DB) {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		be.Migrated().Setup(t, c)
		c.DMR.DisableRadioIDValidation = false
		c.DMR.RadioIDChecks = checks
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	return router, tdb.DB()
}

func TestRadioIDChecksTurnOnAndOffOneByOne(t *testing.T) {
	t.Parallel()
	all := config.RadioIDChecks{UserID: true, Callsign: true, RadioIDs: true, RepeaterIDs: true, HotspotIDs: true}
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()

			t.Run("user-id off", func(t *testing.T) {
				t.Parallel()
				c := all
				c.UserID, c.Callsign = false, false
				router, _ := checksRouter(t, be, c)
				resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 99999999, Callsign: "N0NONE", Username: "nolist", Password: "password"})
				assert.Equal(t, http.StatusOK, w.Code, resp.Error)
			})

			t.Run("user-id on", func(t *testing.T) {
				t.Parallel()
				c := all
				c.Callsign = false
				router, _ := checksRouter(t, be, c)
				resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 99999999, Callsign: "N0NONE", Username: "nolist", Password: "password"})
				assert.Equal(t, http.StatusBadRequest, w.Code)
				assert.Equal(t, "DMR ID is not valid", resp.Error)
				resp, w = testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "N0CALL", Username: "othercall", Password: "password"})
				assert.Equal(t, http.StatusOK, w.Code, "callsign off: %s", resp.Error)
			})

			t.Run("callsign on", func(t *testing.T) {
				t.Parallel()
				c := all
				c.UserID = false
				router, _ := checksRouter(t, be, c)
				resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "N0CALL", Username: "othercall", Password: "password"})
				assert.Equal(t, http.StatusBadRequest, w.Code)
				assert.Equal(t, "Callsign does not match DMR ID", resp.Error)
				_, _, jar := testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "rightcall", Password: "password"})
				resp, w = testutils.PatchUser(t, router, 3191868, jar, apimodels.UserPatch{Callsign: "N0CALL"})
				assert.Equal(t, http.StatusBadRequest, w.Code)
				assert.Equal(t, "Callsign does not match DMR ID", resp.Error)
			})

			t.Run("callsign off", func(t *testing.T) {
				t.Parallel()
				c := all
				c.Callsign = false
				router, _ := checksRouter(t, be, c)
				_, _, jar := testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "rightcall", Password: "password"})
				resp, w := testutils.PatchUser(t, router, 3191868, jar, apimodels.UserPatch{Callsign: "N0CALL"})
				assert.Equal(t, http.StatusOK, w.Code, resp.Error)
			})

			for _, on := range []bool{true, false} {
				t.Run(fmt.Sprintf("radio-ids %v", on), func(t *testing.T) {
					t.Parallel()
					c := all
					c.RadioIDs = on
					router, db := checksRouter(t, be, c)
					testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
					jar := testutils.Login(t, router, "owner")
					w := testutils.Do(t, router, jar, http.MethodPost, "/api/v1/users/me/radio-ids", apimodels.RadioIDPost{ID: 3000002})
					if on {
						assert.Equal(t, http.StatusBadRequest, w.Code)
						assert.Contains(t, w.Body.String(), "does not list 3000002 under your callsign")
					} else {
						assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
					}
				})
			}
		})
	}
}

func TestOldSwitchTurnsOffEveryRadioIDCheck(t *testing.T) {
	t.Parallel()
	d := config.DMR{RadioIDChecks: config.RadioIDChecks{UserID: true, Callsign: true, RadioIDs: true, RepeaterIDs: true, HotspotIDs: true}}
	assert.Equal(t, d.RadioIDChecks, d.Checks())
	assert.True(t, d.Checks().Drift())
	d.DisableRadioIDValidation = true
	assert.Equal(t, config.RadioIDChecks{}, d.Checks())
	assert.False(t, d.Checks().Drift())
}
