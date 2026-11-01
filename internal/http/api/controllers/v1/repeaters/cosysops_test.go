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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type cosysopEnv struct {
	router *gin.Engine
	db     *gorm.DB
	owner  testutils.CookieJar
	kevin  testutils.CookieJar
	other  testutils.CookieJar
}

const cosysopRepeater = 319186801

func newCosysopEnv(t *testing.T) *cosysopEnv {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	database := tdb.DB()
	owner := testutils.SeedAccount(t, database, 3191868, "KP4DJT", "kp4djt")
	testutils.SeedAccount(t, database, 3191869, "KI5VMF", "kevin")
	testutils.SeedAccount(t, database, 3191870, "N0CALL", "other")
	require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: cosysopRepeater, Callsign: "KP4DJT"}, Type: models.RepeaterTypeMMDVM, Password: "password1234", OwnerID: owner.ID}).Error)
	return &cosysopEnv{
		router: router,
		db:     database,
		owner:  testutils.Login(t, router, "kp4djt"),
		kevin:  testutils.Login(t, router, "kevin"),
		other:  testutils.Login(t, router, "other"),
	}
}

func cosysopsOf(t *testing.T, e *cosysopEnv, jar testutils.CookieJar) (int, apimodels.Cosysops) {
	t.Helper()
	w := testutils.Do(t, e.router, jar, http.MethodGet, "/api/v1/repeaters/319186801/cosysops", nil)
	var out apimodels.Cosysops
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	}
	return w.Code, out
}

func invitesOf(t *testing.T, e *cosysopEnv, jar testutils.CookieJar) []apimodels.CosysopInvite {
	t.Helper()
	w := testutils.Do(t, e.router, jar, http.MethodGet, "/api/v1/repeaters/cosysop-invites", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		Invites []apimodels.CosysopInvite `json:"invites"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out.Invites
}

func patchCodec(t *testing.T, e *cosysopEnv, jar testutils.CookieJar) int {
	t.Helper()
	return testutils.Do(t, e.router, jar, http.MethodPatch, "/api/v1/repeaters/319186801", codecPatch("codec2", "auto")).Code
}

// TestACosysopJoinsByInviteAndAccept invites a user as the owner, checks
// that the invite gives no rights until the user accepts it, and that a
// co-sysop then manages the repeater but cannot rotate its password,
// delete it or manage the other co-sysops.
func TestACosysopJoinsByInviteAndAccept(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newCosysopEnv(t)

		w := testutils.Do(t, e.router, e.owner, http.MethodPost, "/api/v1/repeaters/319186801/cosysops", apimodels.CosysopPost{User: "ki5vmf"})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

		code, list := cosysopsOf(t, e, e.owner)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "owner", list.Role)
		require.Len(t, list.Cosysops, 1)
		require.True(t, list.Cosysops[0].Pending)
		require.Equal(t, "KI5VMF", list.Cosysops[0].Callsign)
		require.NotNil(t, list.Cosysops[0].ExpiresAt)
		require.Equal(t, 7*24*time.Hour, list.Cosysops[0].ExpiresAt.Sub(list.Cosysops[0].InvitedAt))

		require.Equal(t, http.StatusForbidden, patchCodec(t, e, e.kevin), "an invite alone gives no rights")
		code, _ = cosysopsOf(t, e, e.kevin)
		require.Equal(t, http.StatusForbidden, code)

		invites := invitesOf(t, e, e.kevin)
		require.Len(t, invites, 1)
		require.Equal(t, uint(cosysopRepeater), invites[0].RepeaterID)
		require.Equal(t, "KP4DJT", invites[0].Owner.Callsign)
		require.Empty(t, invitesOf(t, e, e.other))

		w = testutils.Do(t, e.router, e.other, http.MethodPost, "/api/v1/repeaters/319186801/cosysop-invite/accept", nil)
		require.Equal(t, http.StatusNotFound, w.Code, "only the invited user accepts")
		w = testutils.Do(t, e.router, e.kevin, http.MethodPost, "/api/v1/repeaters/319186801/cosysop-invite/accept", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Empty(t, invitesOf(t, e, e.kevin))

		require.Equal(t, http.StatusOK, patchCodec(t, e, e.kevin))
		w = testutils.Do(t, e.router, e.kevin, http.MethodGet, "/api/v1/repeaters/319186801/rules", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		w = testutils.Do(t, e.router, e.kevin, http.MethodGet, "/api/v1/lastheard/repeater/319186801", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		code, list = cosysopsOf(t, e, e.kevin)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "cosysop", list.Role)
		require.Equal(t, "KP4DJT", list.Owner.Callsign)
		require.Len(t, list.Cosysops, 1)
		require.False(t, list.Cosysops[0].Pending)

		w = testutils.Do(t, e.router, e.kevin, http.MethodGet, "/api/v1/repeaters/my", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var my struct {
			Total     int               `json:"total"`
			Repeaters []models.Repeater `json:"repeaters"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &my))
		require.Equal(t, 1, my.Total)
		require.Len(t, my.Repeaters, 1)
		require.Equal(t, uint(cosysopRepeater), my.Repeaters[0].ID)

		w = testutils.Do(t, e.router, e.kevin, http.MethodPost, "/api/v1/repeaters/319186801/rotate-password", nil)
		require.Equal(t, http.StatusForbidden, w.Code)
		w = testutils.Do(t, e.router, e.kevin, http.MethodDelete, "/api/v1/repeaters/319186801", nil)
		require.Equal(t, http.StatusForbidden, w.Code)
		w = testutils.Do(t, e.router, e.kevin, http.MethodPost, "/api/v1/repeaters/319186801/cosysops", apimodels.CosysopPost{User: "other"})
		require.Equal(t, http.StatusForbidden, w.Code)

		require.Equal(t, http.StatusForbidden, patchCodec(t, e, e.other))
		code, _ = cosysopsOf(t, e, e.other)
		require.Equal(t, http.StatusForbidden, code)
	})
}

// TestACosysopInviteExpiresAfterSevenDays moves the end of an invite to
// one second after now and then to now, as after
// models.CosysopInviteLifetime: it disappears from both sides, cannot be
// accepted, and the owner can invite the user again.
func TestACosysopInviteExpiresAfterSevenDays(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newCosysopEnv(t)
		w := testutils.Do(t, e.router, e.owner, http.MethodPost, "/api/v1/repeaters/319186801/cosysops", apimodels.CosysopPost{User: "KI5VMF"})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		w = testutils.Do(t, e.router, e.owner, http.MethodPost, "/api/v1/repeaters/319186801/cosysops", apimodels.CosysopPost{User: "kevin"})
		require.Equal(t, http.StatusConflict, w.Code, "a second invite while one waits")

		age := func(by time.Duration) {
			t.Helper()
			require.NoError(t, e.db.Model(&models.RepeaterCosysop{}).Where("user_id = ?", 3191869).
				Update("expires_at", time.Now().Add(models.CosysopInviteLifetime-by).UTC()).Error)
		}
		age(models.CosysopInviteLifetime - time.Second)
		require.Len(t, invitesOf(t, e, e.kevin), 1)
		age(models.CosysopInviteLifetime)

		require.Empty(t, invitesOf(t, e, e.kevin))
		_, list := cosysopsOf(t, e, e.owner)
		require.Empty(t, list.Cosysops)
		w = testutils.Do(t, e.router, e.kevin, http.MethodPost, "/api/v1/repeaters/319186801/cosysop-invite/accept", nil)
		require.Equal(t, http.StatusNotFound, w.Code)
		require.Equal(t, http.StatusForbidden, patchCodec(t, e, e.kevin))

		w = testutils.Do(t, e.router, e.owner, http.MethodPost, "/api/v1/repeaters/319186801/cosysops", apimodels.CosysopPost{User: "kevin"})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		require.Len(t, invitesOf(t, e, e.kevin), 1)
	})
}

// TestCosysopInvitesAreRefusedOrRemoved covers a decline, an invite of the
// owner or an unknown user, a co-sysop who leaves, and an owner who
// removes one.
func TestCosysopInvitesAreRefusedOrRemoved(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newCosysopEnv(t)
		invite := func(user string) int {
			return testutils.Do(t, e.router, e.owner, http.MethodPost, "/api/v1/repeaters/319186801/cosysops", apimodels.CosysopPost{User: user}).Code
		}
		require.Equal(t, http.StatusBadRequest, invite("KP4DJT"))
		require.Equal(t, http.StatusBadRequest, invite("nobody"))
		require.Equal(t, http.StatusBadRequest, invite(""))

		require.Equal(t, http.StatusCreated, invite("kevin"))
		w := testutils.Do(t, e.router, e.kevin, http.MethodPost, "/api/v1/repeaters/319186801/cosysop-invite/decline", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		_, list := cosysopsOf(t, e, e.owner)
		require.Empty(t, list.Cosysops)

		require.Equal(t, http.StatusCreated, invite("kevin"))
		require.Equal(t, http.StatusCreated, invite("other"))
		for _, jar := range []testutils.CookieJar{e.kevin, e.other} {
			w = testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/repeaters/319186801/cosysop-invite/accept", nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		}
		w = testutils.Do(t, e.router, e.kevin, http.MethodDelete, "/api/v1/repeaters/319186801/cosysops/3191870", nil)
		require.Equal(t, http.StatusForbidden, w.Code, "a co-sysop removes only themselves")
		w = testutils.Do(t, e.router, e.kevin, http.MethodDelete, "/api/v1/repeaters/319186801/cosysops/3191869", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, http.StatusForbidden, patchCodec(t, e, e.kevin))
		w = testutils.Do(t, e.router, e.owner, http.MethodDelete, "/api/v1/repeaters/319186801/cosysops/3191870", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, http.StatusForbidden, patchCodec(t, e, e.other))
		w = testutils.Do(t, e.router, e.owner, http.MethodDelete, "/api/v1/repeaters/319186801/cosysops/3191870", nil)
		require.Equal(t, http.StatusNotFound, w.Code)

		_, _, admin := testutils.LoginAdmin(t, e.router)
		code, list := cosysopsOf(t, e, admin)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "admin", list.Role)
	})
}

// TestDeletingARepeaterOrAUserEndsItsCosysops deletes a repeater and a
// user, and checks that no co-sysop row of either is left.
func TestDeletingARepeaterOrAUserEndsItsCosysops(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newCosysopEnv(t)
		now := time.Now()
		_, err := models.InviteCosysop(e.db, cosysopRepeater, 3191869, 3191868, now)
		require.NoError(t, err)
		_, err = models.AnswerCosysopInvite(e.db, cosysopRepeater, 3191869, true, now)
		require.NoError(t, err)
		require.NoError(t, e.db.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 319186901}, Type: models.RepeaterTypeMMDVM, Password: "password1234", OwnerID: 3191870}).Error)
		_, err = models.InviteCosysop(e.db, 319186901, 3191869, 3191870, now)
		require.NoError(t, err)

		require.NoError(t, models.DeleteRepeater(e.db, cosysopRepeater))
		var n int64
		require.NoError(t, e.db.Model(&models.RepeaterCosysop{}).Where("repeater_id = ?", cosysopRepeater).Count(&n).Error)
		require.Zero(t, n)

		require.NoError(t, models.DeleteUser(e.db, 3191869))
		require.NoError(t, e.db.Model(&models.RepeaterCosysop{}).Count(&n).Error)
		require.Zero(t, n)
	})
}
