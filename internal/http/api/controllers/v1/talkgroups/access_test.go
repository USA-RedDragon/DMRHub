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

package talkgroups_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	accessTG     = uint(50001)
	tgOwner      = uint(3191868)
	repeaterUser = uint(3191869)
	ownRepeater  = uint(319186901)
)

type accessEnv struct {
	router             *gin.Engine
	owner, user, admin testutils.CookieJar
}

func newAccessEnv(t *testing.T, be testutils.Backend) *accessEnv {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) { be.Migrated().Setup(t, c) })
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	e := &accessEnv{router: router}
	_, _, e.owner = testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: tgOwner, Callsign: "KI5VMF", Username: "owner", Password: "password"})
	_, _, e.user = testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: repeaterUser, Callsign: "N0CALL", Username: "user", Password: "password"})
	_, _, e.admin = testutils.LoginAdmin(t, router)
	_, w := testutils.CreateTalkgroup(t, router, e.admin, apimodels.TalkgroupPost{ID: accessTG, Name: "Club", Description: "Club talkgroup"})
	require.Equal(t, http.StatusOK, w.Code)
	_, w = testutils.SetTalkgroupAdmins(t, router, accessTG, e.admin, apimodels.TalkgroupAdminAction{UserIDs: []uint{tgOwner}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, w = testutils.CreateRepeater(t, router, e.user, apimodels.RepeaterPost{RadioID: ownRepeater, Type: "mmdvm"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return e
}

func (e *accessEnv) do(t *testing.T, method, path string, jar testutils.CookieJar, body string) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	e.router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func (e *accessEnv) access(t *testing.T, method string, jar testutils.CookieJar, body string) (int, apimodels.TalkgroupAccess, string) {
	t.Helper()
	code, out := e.do(t, method, fmt.Sprintf("/api/v1/talkgroups/%d/access", accessTG), jar, body)
	var a apimodels.TalkgroupAccess
	var answer struct {
		Error string `json:"error"`
	}
	if code == http.StatusOK {
		require.NoError(t, json.Unmarshal([]byte(out), &a))
	} else {
		_ = json.Unmarshal([]byte(out), &answer)
	}
	return code, a, answer.Error
}

func accessBackends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

func TestATalkgroupOwnerRestrictsTheTalkgroup(t *testing.T) {
	t.Parallel()
	for _, be := range accessBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newAccessEnv(t, be)

			code, a, _ := e.access(t, http.MethodGet, e.owner, "")
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, apimodels.TalkgroupAccess{Mode: apimodels.TalkgroupAccessOpen, Repeaters: []apimodels.RepeaterRef{}}, a)

			code, _, _ = e.access(t, http.MethodPatch, e.user, `{"mode":"approved"}`)
			assert.Equal(t, http.StatusForbidden, code, "a user who does not own the talkgroup")

			code, a, msg := e.access(t, http.MethodPatch, e.owner, fmt.Sprintf(`{"mode":"approved","repeaters":[%d]}`, ownRepeater))
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, apimodels.TalkgroupAccessApproved, a.Mode)
			assert.Equal(t, []apimodels.RepeaterRef{{ID: ownRepeater, Callsign: "N0CALL"}}, a.Repeaters)

			code, a, msg = e.access(t, http.MethodPatch, e.admin, `{"repeaters":[]}`)
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, apimodels.TalkgroupAccessApproved, a.Mode, "a field left out keeps its value")
			assert.Empty(t, a.Repeaters)

			for body, want := range map[string]string{
				`{"mode":"closed"}`:      "Mode must be open or approved",
				`{"repeaters":[311999]}`: "Repeater does not exist",
				`{"mode":`:               "JSON data is invalid",
			} {
				code, _, msg = e.access(t, http.MethodPatch, e.owner, body)
				assert.Equal(t, http.StatusBadRequest, code, body)
				assert.Equal(t, want, msg, body)
			}

			code, a, msg = e.access(t, http.MethodPatch, e.owner, `{"mode":"open"}`)
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, apimodels.TalkgroupAccessOpen, a.Mode)
		})
	}
}

func TestOnlyAnApprovedRepeaterLinksARestrictedTalkgroup(t *testing.T) {
	t.Parallel()
	for _, be := range accessBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newAccessEnv(t, be)
			code, _, msg := e.access(t, http.MethodPatch, e.owner, `{"mode":"approved"}`)
			require.Equal(t, http.StatusOK, code, msg)

			link := func(kind string) int {
				t.Helper()
				code, _ := e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/repeaters/%d/link/%s/2/%d", ownRepeater, kind, accessTG), e.user, "")
				return code
			}
			set := func(body string) (int, string) {
				t.Helper()
				return e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/repeaters/%d/talkgroups", ownRepeater), e.user, body)
			}
			refused := fmt.Sprintf("Talkgroup %d links only to approved repeaters", accessTG)
			assert.Equal(t, http.StatusForbidden, link("static"))
			assert.Equal(t, http.StatusForbidden, link("dynamic"))
			code, out := set(fmt.Sprintf(`{"ts1_static_talkgroups":[{"id":%d}],"ts2_static_talkgroups":[]}`, accessTG))
			assert.Equal(t, http.StatusForbidden, code)
			assert.Contains(t, out, refused)
			code, out = set(fmt.Sprintf(`{"ts1_static_talkgroups":[],"ts2_static_talkgroups":[],"ts2_dynamic_talkgroup":{"id":%d}}`, accessTG))
			assert.Equal(t, http.StatusForbidden, code)
			assert.Contains(t, out, refused)

			code, _, msg = e.access(t, http.MethodPatch, e.owner, fmt.Sprintf(`{"repeaters":[%d]}`, ownRepeater))
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, http.StatusOK, link("static"))
			code, out = set(fmt.Sprintf(`{"ts1_static_talkgroups":[{"id":%d}],"ts2_static_talkgroups":[]}`, accessTG))
			assert.Equal(t, http.StatusOK, code, out)
		})
	}
}
