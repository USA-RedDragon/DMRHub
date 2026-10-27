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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allBackends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

type patchAnswer struct {
	Message         string `json:"message"`
	Error           string `json:"error"`
	RadioIDMismatch bool   `json:"radio_id_mismatch"`
	RadioIDCallsign string `json:"radio_id_callsign"`
}

func patchAnswerOf(t *testing.T, body []byte) patchAnswer {
	t.Helper()
	var a patchAnswer
	require.NoError(t, json.Unmarshal(body, &a))
	return a
}

func TestPATCHUserCallsign(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
				be.Migrated().Setup(t, c)
				c.DMR.DisableRadioIDValidation = false
			})
			require.NoError(t, err)
			defer tdb.CloseDB()
			database := tdb.DB()

			user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
			_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
			_, _, adminJar := testutils.LoginAdmin(t, router)
			require.NoError(t, database.Model(&models.User{}).Where("id = ?", user.DMRId).UpdateColumn("callsign", "OLDCALL").Error)

			stored := func() models.User {
				t.Helper()
				u, err := models.FindUserWithoutRepeaters(database, user.DMRId)
				require.NoError(t, err)
				return u
			}

			_, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "N0CALL"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			a := patchAnswerOf(t, w.Body.Bytes())
			assert.Equal(t, "Callsign does not match DMR ID", a.Error)
			assert.False(t, a.RadioIDMismatch, "only an admin can override")

			_, w = testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "N0CALL", OverrideRadioID: true})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "OLDCALL", stored().Callsign)

			require.NoError(t, models.ReplaceRadioIDDrifts(database, []models.RadioIDDrift{{RadioID: user.DMRId, UserID: user.DMRId, RadioIDCallsign: "KI5VMF"}}, time.Now()))
			resp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "ki5vmf"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			assert.Equal(t, "KI5VMF", stored().Callsign)
			_, drifted, err := models.FindRadioIDDrift(database, user.DMRId)
			require.NoError(t, err)
			assert.False(t, drifted, "a new callsign ends the drift")

			_, w = testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{Callsign: "N0CALL"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			a = patchAnswerOf(t, w.Body.Bytes())
			assert.True(t, a.RadioIDMismatch)
			assert.Equal(t, "KI5VMF", a.RadioIDCallsign)
			assert.Equal(t, "KI5VMF", stored().Callsign)

			resp, w = testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{Callsign: "n0call", OverrideRadioID: true})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			u := stored()
			assert.Equal(t, "N0CALL", u.Callsign)
			assert.True(t, u.CallsignOverride)

			resp, w = testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{Username: "renamed"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			assert.True(t, stored().CallsignOverride, "an edit that keeps the callsign keeps the override")

			resp, w = testutils.PatchUser(t, router, user.DMRId, adminJar, apimodels.UserPatch{Callsign: "KI5VMF"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			u = stored()
			assert.Equal(t, "KI5VMF", u.Callsign)
			assert.False(t, u.CallsignOverride)
		})
	}
}

func TestPATCHUserCallsignWithoutRadioIDValidation(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) { be.Migrated().Setup(t, c) })
			require.NoError(t, err)
			defer tdb.CloseDB()
			database := tdb.DB()

			user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
			_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
			require.NoError(t, database.Create(&models.User{ID: 3140001, Callsign: "W1AW", Username: "other", Approved: true}).Error)

			resp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "n0call"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			u, err := models.FindUserWithoutRepeaters(database, user.DMRId)
			require.NoError(t, err)
			assert.Equal(t, "N0CALL", u.Callsign)
			assert.False(t, u.CallsignOverride)

			resp, w = testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Callsign: "w1aw"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "Callsign is already taken", resp.Error)
		})
	}
}

func TestPATCHUserUsernameIsChecked(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)

	for name, want := range map[string]string{
		"ab":                     "Username must be at least 3 characters",
		"has space":              "Username must be alphanumeric, _, -, or .",
		"abcdefghijklmnopqrstuv": "Username must be less than 20 characters",
	} {
		resp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Username: name})
		assert.Equal(t, http.StatusBadRequest, w.Code, name)
		assert.Equal(t, want, resp.Error, name)
	}
	resp, w := testutils.PatchUser(t, router, user.DMRId, userJar, apimodels.UserPatch{Username: "new.name_1"})
	assert.Equal(t, http.StatusOK, w.Code, resp.Error)
}

func TestDeleteUserRemovesItsRadioIDDrift(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			_, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) { be.Migrated().Setup(t, c) })
			require.NoError(t, err)
			defer tdb.CloseDB()
			database := tdb.DB()
			require.NoError(t, database.Create(&models.User{ID: 3140001, Callsign: "W1AW", Username: "other", Approved: true}).Error)
			require.NoError(t, models.ReplaceRadioIDDrifts(database, []models.RadioIDDrift{{RadioID: 3140001, UserID: 3140001, RadioIDCallsign: "N0NEW"}}, time.Now()))
			require.NoError(t, models.DeleteUser(database, 3140001))
			_, drifted, err := models.FindRadioIDDrift(database, 3140001)
			require.NoError(t, err)
			assert.False(t, drifted)
		})
	}
}

type sentMail struct {
	to, subject, body string
}

type fakeMailer struct {
	sent chan sentMail
}

func (f *fakeMailer) SendText(to, subject, body string) error {
	f.sent <- sentMail{to, subject, body}
	return nil
}

func driftRequest(t *testing.T, router *gin.Engine, jar testutils.CookieJar, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func unknownToRadioID(t *testing.T) uint {
	t.Helper()
	for id := uint(9999999); id > 9990000; id-- {
		if _, known := userdb.Get(id); !known {
			return id
		}
	}
	require.FailNow(t, "RadioID has every DMR ID that the test tried")
	return 0
}

func TestRadioIDDriftListAndFix(t *testing.T) {
	t.Parallel()
	unknownDMRID := unknownToRadioID(t)
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			mailer := &fakeMailer{sent: make(chan sentMail, 4)}
			router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) {
				be.Migrated().Setup(t, c)
				c.SMTP.Enabled = true
				c.NetworkName = "Test Hub"
			}, dmrhttp.WithMailer(mailer))
			require.NoError(t, err)
			defer tdb.CloseDB()
			database := tdb.DB()

			user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
			_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
			_, _, adminJar := testutils.LoginAdmin(t, router)
			require.NoError(t, database.Exec("UPDATE users SET callsign = ?, email = ?, email_verified = ? WHERE id = ?", "OLDCALL", "ki5vmf@example.com", true, user.DMRId).Error)
			require.NoError(t, database.Create(&models.User{ID: unknownDMRID, Callsign: "N0GONE", Username: "gone", Approved: true}).Error)
			require.NoError(t, models.ReplaceRadioIDDrifts(database, []models.RadioIDDrift{
				{RadioID: user.DMRId, UserID: user.DMRId, RadioIDCallsign: "KI5VMF"},
				{RadioID: unknownDMRID, UserID: unknownDMRID},
			}, time.Now()))

			w := driftRequest(t, router, userJar, http.MethodGet, "/api/v1/admin/radio-id-drift", "")
			assert.Equal(t, http.StatusForbidden, w.Code)
			w = driftRequest(t, router, userJar, http.MethodPost, "/api/v1/admin/radio-id-drift/3191868/fix", "")
			assert.Equal(t, http.StatusForbidden, w.Code)

			w = driftRequest(t, router, adminJar, http.MethodGet, "/api/v1/admin/radio-id-drift", "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var list apimodels.RadioIDDriftList
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
			require.Equal(t, int64(2), list.Total)
			require.Len(t, list.Drifts, 2)
			byID := map[uint]apimodels.RadioIDDrift{}
			for _, d := range list.Drifts {
				byID[d.RadioID] = d
			}
			assert.Equal(t, apimodels.RadioIDDrift{RadioID: user.DMRId, UserID: user.DMRId, Username: "testuser", Callsign: "OLDCALL", RadioIDCallsign: "KI5VMF", CanFix: true, CanNotify: true},
				withoutTimes(byID[user.DMRId]))
			assert.Equal(t, apimodels.RadioIDDrift{RadioID: unknownDMRID, UserID: unknownDMRID, Username: "gone", Callsign: "N0GONE"}, withoutTimes(byID[unknownDMRID]))

			w = driftRequest(t, router, adminJar, http.MethodPost, fmt.Sprintf("/api/v1/admin/radio-id-drift/%d/fix", unknownDMRID), "")
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "RadioID does not have this DMR ID", patchAnswerOf(t, w.Body.Bytes()).Error)

			require.NoError(t, database.Model(&models.User{}).Where("id = ?", unknownDMRID).UpdateColumn("username", "ki5vmf").Error)
			w = driftRequest(t, router, adminJar, http.MethodPost, "/api/v1/admin/radio-id-drift/3191868/fix", `{"notify":true}`)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "Callsign is the username of another user", patchAnswerOf(t, w.Body.Bytes()).Error)
			require.NoError(t, database.Model(&models.User{}).Where("id = ?", unknownDMRID).UpdateColumn("username", "gone").Error)

			w = driftRequest(t, router, adminJar, http.MethodPost, "/api/v1/admin/radio-id-drift/3191868/fix", `{"notify":true}`)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var fixed apimodels.RadioIDDriftFixed
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fixed))
			assert.Equal(t, apimodels.RadioIDDriftFixed{Message: "User updated", Callsign: "KI5VMF", Notified: true}, fixed)
			u, err := models.FindUserWithoutRepeaters(database, user.DMRId)
			require.NoError(t, err)
			assert.Equal(t, "KI5VMF", u.Callsign)
			_, drifted, err := models.FindRadioIDDrift(database, user.DMRId)
			require.NoError(t, err)
			assert.False(t, drifted)
			select {
			case m := <-mailer.sent:
				assert.Equal(t, "ki5vmf@example.com", m.to)
				assert.Equal(t, "Your Test Hub callsign is now KI5VMF", m.subject)
				assert.Contains(t, m.body, "from OLDCALL to KI5VMF")
			case <-time.After(testTimeout):
				require.FailNow(t, "no callsign notice was sent")
			}
		})
	}
}

func TestRadioIDDriftFixWithoutNotice(t *testing.T) {
	t.Parallel()
	mailer := &fakeMailer{sent: make(chan sentMail, 4)}
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) { c.SMTP.Enabled = true }, dmrhttp.WithMailer(mailer))
	require.NoError(t, err)
	defer tdb.CloseDB()
	database := tdb.DB()
	_, _, adminJar := testutils.LoginAdmin(t, router)
	require.NoError(t, database.Create(&models.User{ID: 3191868, Callsign: "OLDCALL", Username: "testuser", Approved: true}).Error)
	require.NoError(t, models.SetCallsignOverride(database, 3191868, true))

	w := driftRequest(t, router, adminJar, http.MethodPost, "/api/v1/admin/radio-id-drift/3191868/fix", `{"notify":true}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var fixed apimodels.RadioIDDriftFixed
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fixed))
	assert.False(t, fixed.Notified, "the user has no verified address")
	u, err := models.FindUserWithoutRepeaters(database, 3191868)
	require.NoError(t, err)
	assert.Equal(t, "KI5VMF", u.Callsign)
	assert.False(t, u.CallsignOverride)
	assert.Empty(t, mailer.sent)
}

func TestRadioIDDriftFixNeedsRadioIDValidation(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) { c.DMR.DisableRadioIDValidation = true })
	require.NoError(t, err)
	defer tdb.CloseDB()
	_, _, adminJar := testutils.LoginAdmin(t, router)
	w := driftRequest(t, router, adminJar, http.MethodPost, "/api/v1/admin/radio-id-drift/3191868/fix", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func withoutTimes(d apimodels.RadioIDDrift) apimodels.RadioIDDrift {
	d.FoundAt = time.Time{}
	d.CheckedAt = time.Time{}
	return d
}

func twoRadioIDsOfOneCallsign(t *testing.T) (callsign string, first, second, other uint) {
	t.Helper()
	seen := map[string]uint{}
	for id := uint(3100000); id < 3200000; id++ {
		u, ok := userdb.Get(id)
		if !ok || u.ID != id || u.Callsign == "" {
			continue
		}
		c := strings.ToUpper(u.Callsign)
		if prev, ok := seen[c]; ok && first == 0 {
			callsign, first, second = c, prev, id
		} else if first != 0 && c != callsign {
			return callsign, first, second, id
		}
		seen[c] = id
	}
	require.FailNow(t, "the test needs a callsign that RadioID lists with two DMR IDs")
	return "", 0, 0, 0
}

func TestRadioIDDriftFixOfAnAddedRadioID(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) { be.Migrated().Setup(t, c) })
			require.NoError(t, err)
			defer tdb.CloseDB()
			database := tdb.DB()
			_, _, adminJar := testutils.LoginAdmin(t, router)

			callsign, main, same, other := twoRadioIDsOfOneCallsign(t)
			require.NoError(t, database.Create(&models.User{ID: main, Callsign: "OLDCALL", Username: "testuser", Approved: true}).Error)
			for _, rid := range []uint{same, other} {
				require.NoError(t, database.Create(&models.UserRadioID{UserID: main, RadioID: rid, VerifiedAt: time.Now()}).Error)
			}
			otherCall, _ := userdb.Get(other)
			require.NoError(t, models.ReplaceRadioIDDrifts(database, []models.RadioIDDrift{
				{RadioID: main, UserID: main, RadioIDCallsign: callsign},
				{RadioID: same, UserID: main, RadioIDCallsign: callsign},
				{RadioID: other, UserID: main, RadioIDCallsign: strings.ToUpper(otherCall.Callsign)},
			}, time.Now()))

			w := driftRequest(t, router, adminJar, http.MethodGet, "/api/v1/admin/radio-id-drift", "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var list apimodels.RadioIDDriftList
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
			canFix := map[uint]bool{}
			for _, d := range list.Drifts {
				canFix[d.RadioID] = d.CanFix
			}
			assert.Equal(t, map[uint]bool{main: true, same: true, other: false}, canFix)

			w = driftRequest(t, router, adminJar, http.MethodPost, fmt.Sprintf("/api/v1/admin/radio-id-drift/%d/fix", other), "")
			assert.Equal(t, http.StatusBadRequest, w.Code)

			w = driftRequest(t, router, adminJar, http.MethodPost, fmt.Sprintf("/api/v1/admin/radio-id-drift/%d/fix", same), "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			u, err := models.FindUserWithoutRepeaters(database, main)
			require.NoError(t, err)
			assert.Equal(t, callsign, u.Callsign)
			for rid, want := range map[uint]bool{main: false, same: false, other: true} {
				_, drifted, err := models.FindRadioIDDrift(database, rid)
				require.NoError(t, err)
				assert.Equal(t, want, drifted, "radio ID %d", rid)
			}

			w = driftRequest(t, router, adminJar, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d/radio-ids/%d", main, other), "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			_, drifted, err := models.FindRadioIDDrift(database, other)
			require.NoError(t, err)
			assert.False(t, drifted, "a removed radio ID leaves the list")
		})
	}
}
