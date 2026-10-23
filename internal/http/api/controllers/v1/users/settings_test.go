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
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	ownerAccountID  = uint(1000001)
	ownerCallsign   = "KQ4APS"
	ownerSecondID   = uint(3191877)
	otherCallsignID = uint(3191869)
)

func settingsRouter(t *testing.T, validation bool) (*gin.Engine, *gorm.DB) {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		c.DMR.DisableRadioIDValidation = !validation
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	return router, tdb.DB()
}

func smsStorage(t *testing.T, router *gin.Engine, jar testutils.CookieJar, on bool) apimodels.UserSettings {
	t.Helper()
	w := testutils.Do(t, router, jar, http.MethodPut, "/api/v1/users/me/settings/sms-storage", map[string]bool{"enabled": on})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var s apimodels.UserSettings
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))
	return s
}

func since(t *testing.T, db *gorm.DB, id uint) *time.Time {
	t.Helper()
	var u models.User
	require.NoError(t, db.First(&u, id).Error)
	return u.SMSStorageSince
}

func TestSMSStorageIsOffByDefault(t *testing.T) {
	t.Parallel()
	router, db := settingsRouter(t, true)
	testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
	jar := testutils.Login(t, router, "owner")

	w := testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"sms_storage": false}`, w.Body.String())
}

func TestSMSStorageOnSetsTheTimeAndOffAndOnResetsIt(t *testing.T) {
	t.Parallel()
	router, db := settingsRouter(t, true)
	testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
	jar := testutils.Login(t, router, "owner")

	before := time.Now().Add(-time.Second)
	assert.True(t, smsStorage(t, router, jar, true).SMSStorage)
	first := since(t, db, ownerAccountID)
	require.NotNil(t, first)
	assert.True(t, first.After(before))

	time.Sleep(20 * time.Millisecond)
	assert.True(t, smsStorage(t, router, jar, true).SMSStorage)
	again := since(t, db, ownerAccountID)
	require.NotNil(t, again)
	assert.True(t, first.Equal(*again), "turning it on again while on keeps the time")

	assert.False(t, smsStorage(t, router, jar, false).SMSStorage)
	time.Sleep(20 * time.Millisecond)
	assert.True(t, smsStorage(t, router, jar, true).SMSStorage)
	reset := since(t, db, ownerAccountID)
	require.NotNil(t, reset)
	assert.True(t, reset.After(*first), "off and on again starts from the new time")
}

func TestSettingsResponsesHaveNoTimeOrEmail(t *testing.T) {
	t.Parallel()
	router, db := settingsRouter(t, true)
	testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
	jar := testutils.Login(t, router, "owner")
	smsStorage(t, router, jar, true)

	for _, path := range []string{"/api/v1/users/me/settings", "/api/v1/users/me/radio-ids", "/api/v1/users/me"} {
		w := testutils.Do(t, router, jar, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		body := strings.ToLower(w.Body.String())
		assert.NotContains(t, body, "sms_storage_", path)
		assert.NotContains(t, body, "since", path)
	}
	w := testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me/settings", nil)
	assert.NotContains(t, w.Body.String(), "email")
}

func TestSMSStorageNeedsLoginAndABody(t *testing.T) {
	t.Parallel()
	router, db := settingsRouter(t, true)
	testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodGet, "/api/v1/users/me/settings", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPut, "/api/v1/users/me/settings/sms-storage", map[string]bool{"enabled": true})
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	jar := testutils.Login(t, router, "owner")
	w = testutils.Do(t, router, jar, http.MethodPut, "/api/v1/users/me/settings/sms-storage", map[string]string{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func radioIDs(t *testing.T, router *gin.Engine, jar testutils.CookieJar, path string) []apimodels.RadioID {
	t.Helper()
	w := testutils.Do(t, router, jar, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var l apimodels.RadioIDList
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &l))
	return l.RadioIDs
}

func TestAddVerifiedRadioID(t *testing.T) {
	t.Parallel()
	router, db := settingsRouter(t, true)
	testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
	jar := testutils.Login(t, router, "owner")

	ids := radioIDs(t, router, jar, "/api/v1/users/me/radio-ids")
	require.Len(t, ids, 1)
	assert.Equal(t, apimodels.RadioID{ID: ownerAccountID, Callsign: ownerCallsign, Account: true}, ids[0])

	w := testutils.Do(t, router, jar, http.MethodPost, "/api/v1/users/me/radio-ids", apimodels.RadioIDPost{ID: ownerSecondID})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var added apimodels.RadioID
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
	assert.Equal(t, ownerSecondID, added.ID)
	assert.Equal(t, ownerCallsign, added.Callsign)
	assert.False(t, added.Account)
	require.NotNil(t, added.VerifiedAt)

	ids = radioIDs(t, router, jar, "/api/v1/users/me/radio-ids")
	require.Len(t, ids, 2)
	assert.Equal(t, ownerSecondID, ids[1].ID)

	w = testutils.Do(t, router, jar, http.MethodDelete, "/api/v1/users/me/radio-ids/3191877", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, radioIDs(t, router, jar, "/api/v1/users/me/radio-ids"), 1)
}

func TestAddRadioIDErrors(t *testing.T) {
	t.Parallel()
	router, db := settingsRouter(t, true)
	testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
	testutils.SeedAccount(t, db, otherCallsignID, "KC1RHO", "other")
	jar := testutils.Login(t, router, "owner")
	require.NoError(t, db.Create(&models.UserRadioID{UserID: otherCallsignID, RadioID: 3191870, VerifiedAt: time.Now()}).Error)

	cases := []struct {
		body any
		code int
		text string
	}{
		{apimodels.RadioIDPost{ID: 319186801}, http.StatusBadRequest, "Enter a 7-digit DMR ID"},
		{map[string]string{"id": "x"}, http.StatusBadRequest, "Enter a 7-digit DMR ID"},
		{apimodels.RadioIDPost{ID: ownerAccountID}, http.StatusBadRequest, "1000001 is already on your list"},
		{apimodels.RadioIDPost{ID: otherCallsignID}, http.StatusBadRequest, "3191869 belongs to another DMRHub user"},
		{apimodels.RadioIDPost{ID: 3191870}, http.StatusBadRequest, "3191870 belongs to another DMRHub user"},
		{apimodels.RadioIDPost{ID: 3191868}, http.StatusBadRequest, "RadioID.net does not list 3191868 under your callsign"},
	}
	for _, tc := range cases {
		w := testutils.Do(t, router, jar, http.MethodPost, "/api/v1/users/me/radio-ids", tc.body)
		assert.Equal(t, tc.code, w.Code, "%v", tc.body)
		var e map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &e))
		assert.Equal(t, tc.text, e["error"], "%v", tc.body)
	}

	w := testutils.Do(t, router, jar, http.MethodPost, "/api/v1/users/me/radio-ids", apimodels.RadioIDPost{ID: ownerSecondID})
	require.Equal(t, http.StatusCreated, w.Code)
	w = testutils.Do(t, router, jar, http.MethodPost, "/api/v1/users/me/radio-ids", apimodels.RadioIDPost{ID: ownerSecondID})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = testutils.Do(t, router, jar, http.MethodDelete, "/api/v1/users/me/radio-ids/1000001", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w = testutils.Do(t, router, jar, http.MethodDelete, "/api/v1/users/me/radio-ids/3191870", nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "another user's ID is not on this list")
	var n int64
	require.NoError(t, db.Model(&models.UserRadioID{}).Where("radio_id = ?", 3191870).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

func TestOnlyAdminsAddRadioIDsWithoutValidation(t *testing.T) {
	t.Parallel()
	router, db := settingsRouter(t, false)
	testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
	jar := testutils.Login(t, router, "owner")

	w := testutils.Do(t, router, jar, http.MethodPost, "/api/v1/users/me/radio-ids", apimodels.RadioIDPost{ID: ownerSecondID})
	assert.Equal(t, http.StatusForbidden, w.Code)
	w = testutils.Do(t, router, jar, http.MethodPost, "/api/v1/users/1000001/radio-ids", apimodels.RadioIDPost{ID: 3191868})
	assert.Equal(t, http.StatusUnauthorized, w.Code, "a user is not an admin")
	w = testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/1000001/radio-ids", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	admin := testutils.Login(t, router, "Admin")
	w = testutils.Do(t, router, admin, http.MethodPost, "/api/v1/users/1000001/radio-ids", apimodels.RadioIDPost{ID: 3191868})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	ids := radioIDs(t, router, admin, "/api/v1/users/1000001/radio-ids")
	require.Len(t, ids, 2)
	assert.Equal(t, uint(3191868), ids[1].ID)
	assert.Len(t, radioIDs(t, router, jar, "/api/v1/users/me/radio-ids"), 2)

	w = testutils.Do(t, router, admin, http.MethodDelete, "/api/v1/users/1000001/radio-ids/3191868", nil)
	require.Equal(t, http.StatusOK, w.Code)
	w = testutils.Do(t, router, admin, http.MethodGet, "/api/v1/users/4242424/radio-ids", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRegistrationRefusesAnAddedRadioID(t *testing.T) {
	t.Parallel()
	router, db := settingsRouter(t, false)
	testutils.SeedAccount(t, db, ownerAccountID, ownerCallsign, "owner")
	require.NoError(t, db.Create(&models.UserRadioID{UserID: ownerAccountID, RadioID: ownerSecondID, VerifiedAt: time.Now()}).Error)
	_, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: ownerSecondID, Callsign: "KQ4APS", Username: "second", Password: "a long pass phrase 123"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "DMR ID is already registered")
}
