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
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	n0call = 9990001
	n1call = 9990002
)

func enrollRouter(t *testing.T, opts ...testutils.ConfigOption) (*gorm.DB, func(jar testutils.CookieJar, post apimodels.RepeaterPost) (int, string), testutils.CookieJar, testutils.CookieJar) {
	t.Helper()
	opts = append([]testutils.ConfigOption{func(c *config.Config) {
		c.DMR.DisableRadioIDValidation = false
		c.DMR.RadioIDChecks = config.RadioIDChecks{RepeaterIDs: true, HotspotIDs: true}
	}}, opts...)
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(opts...)
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	database := tdb.DB()
	testutils.SeedAccount(t, database, n0call, "N0CALL", "n0call")
	testutils.SeedAccount(t, database, n1call, "N1CALL", "n1call")
	_, _, adminJar := testutils.LoginAdmin(t, router)
	userJar := testutils.Login(t, router, "n1call")
	enroll := func(jar testutils.CookieJar, post apimodels.RepeaterPost) (int, string) {
		resp, w := testutils.CreateRepeater(t, router, jar, post)
		return w.Code, resp.Error
	}
	return database, enroll, adminJar, userJar
}

func ownerOf(t *testing.T, database *gorm.DB, id uint) (uint, bool) {
	t.Helper()
	var r models.Repeater
	err := database.First(&r, id).Error
	if err != nil {
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
		return 0, false
	}
	return r.OwnerID, true
}

func TestAnAdminEnrollsAHotspotForAnotherUser(t *testing.T) {
	t.Parallel()
	database, enroll, adminJar, _ := enrollRouter(t)
	code, msg := enroll(adminJar, apimodels.RepeaterPost{RadioID: 999000101, Owner: n0call})
	require.Equal(t, http.StatusOK, code, msg)
	r, err := models.FindRepeaterByID(database, 999000101)
	require.NoError(t, err)
	require.EqualValues(t, n0call, r.OwnerID)
	require.Equal(t, "N0CALL", r.Callsign)
}

func TestTheOwnerOfARepeaterAnAdminEnrollsDefaultsToTheAdmin(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions()
	require.NoError(t, err)
	defer tdb.CloseDB()
	_, _, adminJar := testutils.LoginAdmin(t, router)
	var admin models.User
	require.NoError(t, tdb.DB().Where("username = ?", "Admin").First(&admin).Error)

	resp, w := testutils.CreateRepeater(t, router, adminJar, apimodels.RepeaterPost{RadioID: 990501})
	require.Equal(t, http.StatusOK, w.Code, resp.Error)
	owner, ok := ownerOf(t, tdb.DB(), 990501)
	require.True(t, ok)
	require.Equal(t, admin.ID, owner)

	resp, w = testutils.CreateRepeater(t, router, adminJar, apimodels.RepeaterPost{RadioID: 990502, Owner: admin.ID})
	require.Equal(t, http.StatusOK, w.Code, resp.Error)
	owner, ok = ownerOf(t, tdb.DB(), 990502)
	require.True(t, ok)
	require.Equal(t, admin.ID, owner)
}

func TestAUserCannotEnrollARepeaterForAnotherUser(t *testing.T) {
	t.Parallel()
	database, enroll, _, userJar := enrollRouter(t)
	code, msg := enroll(userJar, apimodels.RepeaterPost{RadioID: 999000101, Owner: n0call})
	require.Equal(t, http.StatusForbidden, code, msg)
	require.Equal(t, "Only an admin can enroll a repeater for another user", msg)
	_, ok := ownerOf(t, database, 999000101)
	require.False(t, ok)

	code, msg = enroll(userJar, apimodels.RepeaterPost{RadioID: 999000201, Owner: n1call})
	require.Equal(t, http.StatusOK, code, msg)
	owner, ok := ownerOf(t, database, 999000201)
	require.True(t, ok)
	require.EqualValues(t, n1call, owner)
}

func TestRadioIDChecksUseTheChosenOwner(t *testing.T) {
	t.Parallel()
	database, enroll, adminJar, _ := enrollRouter(t)
	for _, tc := range []struct {
		name string
		post apimodels.RepeaterPost
		msg  string
	}{
		{"hotspot of another user", apimodels.RepeaterPost{RadioID: 999000201, Owner: n0call}, "Hotspot ID must start with user ID"},
		{"repeater the owner is no trustee of", apimodels.RepeaterPost{RadioID: 999998, Owner: n0call}, "Repeater ID does not match assigned callsign"},
	} {
		code, msg := enroll(adminJar, tc.post)
		require.Equal(t, http.StatusBadRequest, code, tc.name)
		require.Equal(t, tc.msg, msg, tc.name)
		_, ok := ownerOf(t, database, tc.post.RadioID)
		require.False(t, ok, tc.name)
	}
	code, msg := enroll(adminJar, apimodels.RepeaterPost{RadioID: 9990002, Owner: n1call})
	require.Equal(t, http.StatusOK, code, msg)
}

func TestAnAdminCannotEnrollARepeaterForAMissingOrBlockedUser(t *testing.T) {
	t.Parallel()
	database, enroll, adminJar, _ := enrollRouter(t)
	require.NoError(t, database.Model(&models.User{}).Where("id = ?", n0call).Update("suspended", true).Error)
	unapproved := testutils.SeedAccount(t, database, 9990003, "N2CALL", "n2call")
	require.NoError(t, database.Model(&unapproved).Update("approved", false).Error)
	for _, tc := range []struct {
		owner uint
		msg   string
	}{
		{9990009, "Owner does not exist"},
		{n0call, "Owner is suspended"},
		{unapproved.ID, "Owner is not approved"},
	} {
		code, msg := enroll(adminJar, apimodels.RepeaterPost{RadioID: tc.owner*100 + 1, Owner: tc.owner})
		require.Equal(t, http.StatusBadRequest, code, tc.msg)
		require.Equal(t, tc.msg, msg)
		_, ok := ownerOf(t, database, tc.owner*100+1)
		require.False(t, ok, tc.msg)
	}
}

func TestTheQuotaDoesNotStopAnAdminEnrollingForAUser(t *testing.T) {
	t.Parallel()
	database, enroll, adminJar, userJar := enrollRouter(t, func(c *config.Config) { c.DMR.RepeaterQuota = 1 })
	code, msg := enroll(userJar, apimodels.RepeaterPost{RadioID: 999000201})
	require.Equal(t, http.StatusOK, code, msg)
	code, _ = enroll(userJar, apimodels.RepeaterPost{RadioID: 999000202})
	require.Equal(t, http.StatusForbidden, code)
	code, msg = enroll(adminJar, apimodels.RepeaterPost{RadioID: 999000202, Owner: n1call})
	require.Equal(t, http.StatusOK, code, msg)
	owner, ok := ownerOf(t, database, 999000202)
	require.True(t, ok)
	require.EqualValues(t, n1call, owner)
}
