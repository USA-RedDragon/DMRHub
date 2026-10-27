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

package lastheard_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type visibleCall struct {
	TalkerAlias string   `json:"talker_alias"`
	Grid        string   `json:"grid"`
	Latitude    *float64 `json:"latitude"`
	User        struct {
		Callsign string `json:"callsign"`
	} `json:"user"`
}

func TestVisibilityOfThePublicPages(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
				be.Migrated().Setup(t, c)
				testutils.SetupReady(c)
			})
			require.NoError(t, err)
			defer tdb.CloseDB()
			db := tdb.DB()

			_, _, user := testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "src", Password: "password"})
			src, err := models.FindUserByID(db, 3191868)
			require.NoError(t, err)
			tg := models.Talkgroup{ID: 91, Name: "TG91"}
			require.NoError(t, db.Create(&tg).Error)
			lat, lon := 32.7767, -96.797
			srcID, tgID := src.ID, tg.ID
			require.NoError(t, db.Create(&models.Call{StreamID: 1, StartTime: time.Now(), UserID: &srcID, SourceID: src.ID, GroupCall: true, IsToTalkgroup: true, ToTalkgroupID: &tgID, DestinationID: tg.ID, TalkerAlias: "KI5VMF Jacob", Latitude: &lat, Longitude: &lon}).Error)
			admin := testutils.Login(t, router, "Admin")
			visitor := testutils.CookieJar{}

			lastheard := func(jar testutils.CookieJar) visibleCall {
				t.Helper()
				w := testutils.Do(t, router, jar, http.MethodGet, "/api/v1/lastheard", nil)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var res struct {
					Calls []visibleCall `json:"calls"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
				require.Len(t, res.Calls, 1)
				return res.Calls[0]
			}
			name := func(jar testutils.CookieJar) string {
				t.Helper()
				w := testutils.Do(t, router, jar, http.MethodGet, "/api/v1/userdb/3191868", nil)
				require.Equal(t, http.StatusOK, w.Code)
				var e struct {
					FName    string `json:"fname"`
					Callsign string `json:"callsign"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &e))
				assert.Equal(t, "KI5VMF", e.Callsign)
				return e.FName
			}

			call := lastheard(visitor)
			assert.Equal(t, "KI5VMF Jacob", call.TalkerAlias, "public by default")
			assert.NotEmpty(t, call.Grid)
			assert.NotEmpty(t, name(visitor))

			testutils.SaveSetting(t, router, admin, "visibility", "hide-names")
			call = lastheard(visitor)
			assert.Equal(t, "KI5VMF", call.User.Callsign)
			assert.Empty(t, call.TalkerAlias)
			assert.Empty(t, call.Grid)
			assert.Nil(t, call.Latitude)
			assert.Empty(t, name(visitor))
			call = lastheard(user)
			assert.Equal(t, "KI5VMF Jacob", call.TalkerAlias, "a logged-in user sees the names")
			assert.NotEmpty(t, call.Grid)
			assert.NotEmpty(t, name(user))

			testutils.SaveSetting(t, router, admin, "visibility", "login")
			for _, path := range []string{"/api/v1/lastheard", "/api/v1/lastheard/talkgroup/91", "/api/v1/talkgroups", "/api/v1/talkgroups/91", "/api/v1/nets", "/api/v1/nets/scheduled", "/api/v1/userdb/3191868", "/ws/lastheard"} {
				assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, visitor, http.MethodGet, path, nil).Code, path)
			}
			for _, path := range []string{"/api/v1/lastheard", "/api/v1/talkgroups", "/api/v1/nets", "/api/v1/userdb/3191868"} {
				assert.Equal(t, http.StatusOK, testutils.Do(t, router, user, http.MethodGet, path, nil).Code, path)
			}
			w := testutils.Do(t, router, visitor, http.MethodGet, "/api/v1/network/features", nil)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), `"visibility":"login"`)
		})
	}
}
