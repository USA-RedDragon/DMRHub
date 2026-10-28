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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rulesEnv struct {
	router           *gin.Engine
	owner, other     testutils.CookieJar
	admin            testutils.CookieJar
	ownerID, otherID uint
}

const rulesRepeater = uint(319186807)

func newRulesEnv(t *testing.T, be testutils.Backend) *rulesEnv {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		be.Migrated().Setup(t, c)
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	e := &rulesEnv{router: router, ownerID: 3191868, otherID: 3191869}
	_, _, e.owner = testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: e.ownerID, Callsign: "KI5VMF", Username: "owner", Password: "password"})
	_, _, e.other = testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: e.otherID, Callsign: "N0CALL", Username: "other", Password: "password"})
	_, _, e.admin = testutils.LoginAdmin(t, router)
	_, w := testutils.CreateRepeater(t, router, e.owner, apimodels.RepeaterPost{RadioID: rulesRepeater, Type: "mmdvm"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for _, tg := range []uint{50001, 50002} {
		_, w = testutils.CreateTalkgroup(t, router, e.admin, apimodels.TalkgroupPost{ID: tg, Name: fmt.Sprintf("TG %d", tg), Description: "Test talkgroup"})
		require.Equal(t, http.StatusOK, w.Code)
	}
	return e
}

func (e *rulesEnv) do(t *testing.T, method string, jar testutils.CookieJar, body string) (int, apimodels.RepeaterRules, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), method, fmt.Sprintf("/api/v1/repeaters/%d/rules", rulesRepeater), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	e.router.ServeHTTP(w, req)
	var rules apimodels.RepeaterRules
	var answer struct {
		Error string `json:"error"`
	}
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rules))
	} else {
		_ = json.Unmarshal(w.Body.Bytes(), &answer)
	}
	return w.Code, rules, answer.Error
}

func ruleBackends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

func TestAnOwnerLocksASlotToListedUsers(t *testing.T) {
	t.Parallel()
	for _, be := range ruleBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newRulesEnv(t, be)

			code, rules, _ := e.do(t, http.MethodGet, e.owner, "")
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, apimodels.RepeaterRules{
				TS1Users: []models.UserRef{}, TS2Users: []models.UserRef{},
				TalkgroupMode: apimodels.TalkgroupModeOff, Talkgroups: []apimodels.TalkgroupRef{},
			}, rules)

			code, rules, msg := e.do(t, http.MethodPatch, e.owner, fmt.Sprintf(`{"ts2_users":[%d,%d,%d]}`, e.otherID, e.ownerID, e.otherID))
			require.Equal(t, http.StatusOK, code, msg)
			assert.ElementsMatch(t, []models.UserRef{{ID: e.otherID, Callsign: "N0CALL"}, {ID: e.ownerID, Callsign: "KI5VMF"}}, rules.TS2Users)
			assert.Empty(t, rules.TS1Users)

			code, rules, msg = e.do(t, http.MethodPatch, e.admin, fmt.Sprintf(`{"ts1_users":[%d]}`, e.ownerID))
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, []models.UserRef{{ID: e.ownerID, Callsign: "KI5VMF"}}, rules.TS1Users)
			assert.Len(t, rules.TS2Users, 2, "a slot left out keeps its users")

			code, _, msg = e.do(t, http.MethodPatch, e.owner, `{"ts1_users":[3199999]}`)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Equal(t, "User does not exist", msg)

			code, _, _ = e.do(t, http.MethodPatch, e.other, `{"ts1_users":[]}`)
			assert.Equal(t, http.StatusForbidden, code, "a user who does not own the repeater")
			code, _, _ = e.do(t, http.MethodGet, e.other, "")
			assert.Equal(t, http.StatusForbidden, code)

			code, rules, msg = e.do(t, http.MethodPatch, e.owner, `{"ts2_users":[]}`)
			require.Equal(t, http.StatusOK, code, msg)
			assert.Empty(t, rules.TS2Users)
			assert.Len(t, rules.TS1Users, 1)
		})
	}
}

func TestAnOwnerSetsATalkgroupList(t *testing.T) {
	t.Parallel()
	for _, be := range ruleBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newRulesEnv(t, be)

			code, rules, msg := e.do(t, http.MethodPatch, e.owner, `{"talkgroup_mode":"block","talkgroups":[50001,50002]}`)
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, apimodels.TalkgroupModeBlock, rules.TalkgroupMode)
			assert.ElementsMatch(t, []apimodels.TalkgroupRef{{ID: 50001, Name: "TG 50001"}, {ID: 50002, Name: "TG 50002"}}, rules.Talkgroups)

			code, rules, msg = e.do(t, http.MethodPatch, e.owner, `{"talkgroup_mode":"allow","talkgroups":[50002]}`)
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, apimodels.TalkgroupModeAllow, rules.TalkgroupMode)
			assert.Equal(t, []apimodels.TalkgroupRef{{ID: 50002, Name: "TG 50002"}}, rules.Talkgroups, "a new mode replaces the list")

			code, rules, msg = e.do(t, http.MethodPatch, e.owner, fmt.Sprintf(`{"ts1_users":[%d]}`, e.ownerID))
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, apimodels.TalkgroupModeAllow, rules.TalkgroupMode, "a slot edit keeps the talkgroup list")

			for body, want := range map[string]string{
				`{"talkgroups":[50001]}`:                       "Talkgroups need a talkgroup mode",
				`{"talkgroup_mode":"block","talkgroups":[]}`:   "List at least one talkgroup",
				`{"talkgroup_mode":"block"}`:                   "List at least one talkgroup",
				`{"talkgroup_mode":"block","talkgroups":[77]}`: "Talkgroup does not exist",
				`{"talkgroup_mode":"deny","talkgroups":[1]}`:   "Talkgroup mode must be off, block or allow",
				`{"talkgroup_mode":`:                           "JSON data is invalid",
			} {
				code, _, msg = e.do(t, http.MethodPatch, e.owner, body)
				assert.Equal(t, http.StatusBadRequest, code, body)
				assert.Equal(t, want, msg, body)
			}

			code, rules, msg = e.do(t, http.MethodPatch, e.owner, `{"talkgroup_mode":"off"}`)
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, apimodels.TalkgroupModeOff, rules.TalkgroupMode)
			assert.Empty(t, rules.Talkgroups)
			assert.Len(t, rules.TS1Users, 1)
		})
	}
}

func TestAnOwnerFindsAUserForASlotLock(t *testing.T) {
	t.Parallel()
	for _, be := range ruleBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newRulesEnv(t, be)
			find := func(jar testutils.CookieJar, q string) (int, models.UserRef) {
				t.Helper()
				w := httptest.NewRecorder()
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/v1/repeaters/%d/rules/user?q=%s", rulesRepeater, q), nil)
				for _, cookie := range jar.Cookies() {
					req.Header.Add("Cookie", cookie.String())
				}
				e.router.ServeHTTP(w, req)
				var ref models.UserRef
				if w.Code == http.StatusOK {
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ref))
				}
				return w.Code, ref
			}
			want := models.UserRef{ID: e.otherID, Callsign: "N0CALL"}
			for _, q := range []string{"3191869", "n0call", "N0CALL"} {
				code, ref := find(e.owner, q)
				assert.Equal(t, http.StatusOK, code, q)
				assert.Equal(t, want, ref, q)
			}
			for _, q := range []string{"3199999", "N0NE", ""} {
				code, _ := find(e.owner, q)
				assert.Equal(t, http.StatusNotFound, code, q)
			}
			code, _ := find(e.other, "3191868")
			assert.Equal(t, http.StatusForbidden, code)
		})
	}
}
