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

package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type forbiddenRoute struct {
	name, method, path, msg string
}

func forbiddenRoutes() []forbiddenRoute {
	return []forbiddenRoute{
		{"RequireAdmin", http.MethodGet, "/api/v1/repeaters", middleware.MsgNotAdmin},
		{"RequireAdminOrTGOwner", http.MethodGet, "/api/v1/users", middleware.MsgNotAdminOrTalkgroupOwner},
		{"RequireSuperAdmin", http.MethodGet, "/api/v1/users/admins", middleware.MsgNotSuperAdmin},
		{"RequireSelfOrAdmin", http.MethodGet, "/api/v1/users/999999", middleware.MsgNotSelf},
		{"RequirePeerOwnerOrAdmin", http.MethodGet, "/api/v1/peers/1", middleware.MsgNotPeerOwner},
		{"RequireTalkgroupOwnerOrAdmin", http.MethodPatch, "/api/v1/talkgroups/1", middleware.MsgNotTalkgroupAdmin},
		{"RequireNetNCOOrOwnerOrAdmin", http.MethodPost, "/api/v1/nets/1/stop", middleware.MsgNotTalkgroupNCO},
		{"RequireScheduledNetNCOOrOwnerOrAdmin", http.MethodDelete, "/api/v1/nets/scheduled/1", middleware.MsgNotTalkgroupNCO},
	}
}

func seedForbiddenTargets(t *testing.T, tdb *testutils.TestDB) {
	t.Helper()
	db := tdb.DB()
	require.NoError(t, db.Create(&models.Talkgroup{ID: 1, Name: "TG1"}).Error)
	require.NoError(t, db.Create(&models.Peer{ID: 1, OwnerID: 1}).Error)
	require.NoError(t, db.Create(&models.Net{ID: 1, TalkgroupID: 1, Active: true}).Error)
	require.NoError(t, db.Create(&models.ScheduledNet{ID: 1, TalkgroupID: 1, Name: "net"}).Error)
}

func callRoute(t *testing.T, router *gin.Engine, r forbiddenRoute, jar *testutils.CookieJar) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.method, r.path, nil)
	require.NoError(t, err)
	if jar != nil {
		for _, cookie := range jar.Cookies() {
			req.Header.Add("Cookie", cookie.String())
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestASignedInUserWithoutTheRightIsForbidden asks for routes that need a
// right that a signed-in, approved user lacks. Each answers 403 with why,
// while a signed-out, unapproved or suspended user still gets 401.
func TestASignedInUserWithoutTheRightIsForbidden(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	seedForbiddenTargets(t, tdb)

	user := apimodels.UserRegistration{Username: "plain", Password: "testpassword123!", Callsign: "KI5VMF", DMRId: 3191868}
	_, _, jar := testutils.CreateAndLoginUser(t, router, user)

	for _, r := range forbiddenRoutes() {
		w := callRoute(t, router, r, &jar)
		assert.Equal(t, http.StatusForbidden, w.Code, r.name)
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), r.name)
		assert.Equal(t, r.msg, body["error"], r.name)

		assert.Equal(t, http.StatusUnauthorized, callRoute(t, router, r, nil).Code, "%s signed out", r.name)
	}

	require.NoError(t, tdb.DB().Model(&models.User{}).Where("id = ?", user.DMRId).Update("approved", false).Error)
	for _, r := range forbiddenRoutes() {
		assert.Equal(t, http.StatusUnauthorized, callRoute(t, router, r, &jar).Code, "%s unapproved", r.name)
	}

	require.NoError(t, tdb.DB().Model(&models.User{}).Where("id = ?", user.DMRId).Updates(map[string]any{"approved": true, "suspended": true}).Error)
	for _, r := range forbiddenRoutes() {
		assert.Equal(t, http.StatusUnauthorized, callRoute(t, router, r, &jar).Code, "%s suspended", r.name)
	}
}
