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

package localgroups_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
)

const (
	ownerHotspot  = uint(319186801)
	ownerHotspot2 = uint(319186802)
	otherHotspot  = uint(319186901)
)

type env struct {
	router              *gin.Engine
	tdb                 *testutils.TestDB
	owner, other, admin testutils.CookieJar
}

func newEnv(t *testing.T, be testutils.Backend) *env {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		be.Migrated().Setup(t, c)
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	e := &env{router: router, tdb: tdb}
	_, _, e.owner = testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "owner", Password: "password"})
	_, _, e.other = testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: 3191869, Callsign: "W5DFW", Username: "other", Password: "password"})
	_, _, e.admin = testutils.LoginAdmin(t, router)
	for _, r := range []struct {
		jar testutils.CookieJar
		id  uint
	}{{e.owner, ownerHotspot}, {e.owner, ownerHotspot2}, {e.other, otherHotspot}} {
		_, w := testutils.CreateRepeater(t, router, r.jar, apimodels.RepeaterPost{RadioID: r.id, Type: "mmdvm"})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	return e
}

// do sends body to path as jar and answers the status, the error message
// and decodes the answer into out when the status is 200.
func (e *env) do(t *testing.T, method, path string, jar testutils.CookieJar, body string, out any) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), method, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	e.router.ServeHTTP(w, req)
	var answer struct {
		Error string `json:"error"`
	}
	if w.Code == http.StatusOK && out != nil {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), out), w.Body.String())
	} else {
		_ = json.Unmarshal(w.Body.Bytes(), &answer)
	}
	return w.Code, answer.Error
}

func (e *env) create(t *testing.T, jar testutils.CookieJar, name string, tg uint) apimodels.LocalGroup {
	t.Helper()
	var g apimodels.LocalGroup
	code, msg := e.do(t, http.MethodPost, "/localgroups", jar, fmt.Sprintf(`{"name":%q,"talkgroup":%d}`, name, tg), &g)
	require.Equal(t, http.StatusOK, code, msg)
	return g
}

func backends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

func TestAnOwnerMakesALocalGroupAndAnotherOwnerSaysYes(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, be)

			code, msg := e.do(t, http.MethodPost, "/localgroups", e.owner, `{"name":"Club","talkgroup":91}`, nil)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Equal(t, "Pick one of the local talkgroups", msg, "only the local talkgroups of the Setup")
			code, msg = e.do(t, http.MethodPost, "/localgroups", e.owner, `{"name":"  ","talkgroup":8}`, nil)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Equal(t, "Give the group a name of 1 to 64 characters", msg)

			g := e.create(t, e.owner, " North Texas ", 8)
			assert.Equal(t, "North Texas", g.Name)
			assert.True(t, g.Mine)
			assert.True(t, g.CanManage)
			path := fmt.Sprintf("/localgroups/%d", g.ID)

			code, msg = e.do(t, http.MethodPost, path+"/repeaters", e.owner, fmt.Sprintf(`{"repeater_id":%d,"slot":2}`, ownerHotspot), &g)
			require.Equal(t, http.StatusOK, code, msg)
			code, msg = e.do(t, http.MethodPost, path+"/repeaters", e.owner, fmt.Sprintf(`{"repeater_id":%d,"slot":1}`, otherHotspot), &g)
			require.Equal(t, http.StatusOK, code, msg)
			require.Len(t, g.Repeaters, 2)
			assert.Equal(t, apimodels.LocalGroupRepeater{ID: ownerHotspot, Callsign: g.Repeaters[0].Callsign, Owner: models.UserRef{ID: 3191868, Callsign: "KI5VMF"}, Slot: 2, State: models.LocalMemberIn, Mine: true}, g.Repeaters[0],
				"a repeater of the group owner joins at once")
			assert.Equal(t, models.LocalMemberAsked, g.Repeaters[1].State, "a repeater of another owner waits for that owner")
			assert.Equal(t, 1, g.Members)

			code, msg = e.do(t, http.MethodPost, path+"/repeaters", e.owner, fmt.Sprintf(`{"repeater_id":%d}`, ownerHotspot), nil)
			assert.Equal(t, http.StatusConflict, code)
			assert.Equal(t, "This repeater is already in the group", msg)

			var asks []apimodels.RepeaterLocalGroup
			code, msg = e.do(t, http.MethodGet, fmt.Sprintf("/repeaters/%d/localgroups", otherHotspot), e.other, "", &asks)
			require.Equal(t, http.StatusOK, code, msg)
			require.Len(t, asks, 1)
			assert.Equal(t, apimodels.RepeaterLocalGroup{ID: g.ID, Name: "North Texas", Talkgroup: 8, Slot: 1, State: models.LocalMemberAsked,
				Owner: models.UserRef{ID: 3191868, Callsign: "KI5VMF"}, AskedBy: models.UserRef{ID: 3191868, Callsign: "KI5VMF"}, Repeaters: 1}, asks[0])
			code, _ = e.do(t, http.MethodGet, fmt.Sprintf("/repeaters/%d/localgroups", otherHotspot), e.owner, "", &asks)
			require.Equal(t, http.StatusOK, code)
			assert.Empty(t, asks, "only the owner of a repeater sees the requests to add it")

			var list apimodels.LocalGroupList
			code, _ = e.do(t, http.MethodGet, "/localgroups", e.other, "", &list)
			require.Equal(t, http.StatusOK, code)
			require.Len(t, list.Groups, 1, "the owner of a repeater asked to join sees the group")
			assert.False(t, list.Groups[0].CanManage)
			assert.True(t, list.Groups[0].Involved)
			assert.Equal(t, []uint{8, 9}, list.Talkgroups)

			accept := fmt.Sprintf("%s/repeaters/%d/accept", path, otherHotspot)
			code, _ = e.do(t, http.MethodPost, accept, e.owner, "", nil)
			assert.Equal(t, http.StatusForbidden, code, "the group owner cannot say yes for another owner")
			code, _ = e.do(t, http.MethodPatch, path, e.other, `{"name":"Mine now","talkgroup":8}`, nil)
			assert.Equal(t, http.StatusForbidden, code, "the owner of a repeater in the group cannot change it")
			code, msg = e.do(t, http.MethodPost, accept, e.other, "", &g)
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, 2, g.Members)

			rpt, err := models.FindRepeaterByID(e.tdb.DB(), otherHotspot)
			require.NoError(t, err)
			assert.Equal(t, []models.LocalMembership{{GroupID: g.ID, Name: "North Texas", Talkgroup: 8, Slot: false}}, rpt.LocalGroups)

			code, msg = e.do(t, http.MethodPatch, fmt.Sprintf("%s/repeaters/%d", path, otherHotspot), e.other, `{"slot":2}`, &g)
			require.Equal(t, http.StatusOK, code, msg)
			assert.EqualValues(t, 2, g.Repeaters[1].Slot, "the owner of a repeater sets its slot")

			code, msg = e.do(t, http.MethodDelete, fmt.Sprintf("%s/repeaters/%d", path, otherHotspot), e.other, "", nil)
			require.Equal(t, http.StatusOK, code, msg)
			code, msg = e.do(t, http.MethodGet, path, e.other, "", nil)
			assert.Equal(t, http.StatusNotFound, code, "a group with no repeater of the user is not shown to the user")
			assert.Equal(t, "Local group does not exist", msg)
		})
	}
}

func TestARepeaterIsInOneGroupForEachLocalTalkgroupThroughTheAPI(t *testing.T) {
	t.Parallel()
	for _, be := range backends()[:2] {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, be)
			north := e.create(t, e.owner, "North Texas", 8)
			regional := e.create(t, e.owner, "Regional", 9)
			club := e.create(t, e.other, "Club", 8)
			for _, g := range []apimodels.LocalGroup{north, regional} {
				code, msg := e.do(t, http.MethodPost, fmt.Sprintf("/localgroups/%d/repeaters", g.ID), e.owner, fmt.Sprintf(`{"repeater_id":%d}`, ownerHotspot), nil)
				require.Equal(t, http.StatusOK, code, msg)
			}
			code, msg := e.do(t, http.MethodPost, fmt.Sprintf("/localgroups/%d/repeaters", club.ID), e.other, fmt.Sprintf(`{"repeater_id":%d}`, ownerHotspot), nil)
			assert.Equal(t, http.StatusConflict, code)
			assert.Equal(t, "This repeater is already in a local group on that talkgroup", msg)

			code, _ = e.do(t, http.MethodPatch, fmt.Sprintf("/localgroups/%d", regional.ID), e.owner, `{"name":"Regional","talkgroup":8}`, nil)
			assert.Equal(t, http.StatusConflict, code, "a group cannot move onto a number its repeater has another group on")

			code, _ = e.do(t, http.MethodPost, fmt.Sprintf("/localgroups/%d/repeaters", club.ID), e.owner, fmt.Sprintf(`{"repeater_id":%d}`, ownerHotspot2), nil)
			assert.Equal(t, http.StatusForbidden, code, "only the group owner adds repeaters")
			code, _ = e.do(t, http.MethodPost, fmt.Sprintf("/localgroups/%d/repeaters", club.ID), e.other, `{"repeater_id":319186899}`, nil)
			assert.Equal(t, http.StatusNotFound, code)

			var list apimodels.LocalGroupList
			code, _ = e.do(t, http.MethodGet, "/localgroups", e.admin, "", &list)
			require.Equal(t, http.StatusOK, code)
			assert.Len(t, list.Groups, 3, "an admin sees every group")
			code, msg = e.do(t, http.MethodDelete, fmt.Sprintf("/localgroups/%d", club.ID), e.admin, "", nil)
			require.Equal(t, http.StatusOK, code, msg)
			code, _ = e.do(t, http.MethodDelete, fmt.Sprintf("/localgroups/%d", north.ID), e.other, "", nil)
			assert.Equal(t, http.StatusForbidden, code)
			code, _ = e.do(t, http.MethodGet, "/localgroups", e.other, "", &list)
			require.Equal(t, http.StatusOK, code)
			assert.Empty(t, list.Groups)
		})
	}
}

func TestACosysopManagesTheLocalGroupsOfItsRepeater(t *testing.T) {
	t.Parallel()
	for _, be := range backends()[:2] {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, be)
			_, _, cosysop := testutils.CreateAndLoginUser(t, e.router, apimodels.UserRegistration{DMRId: 3191870, Callsign: "N5ARL", Username: "cosysop", Password: "password"})
			now := time.Now()
			require.NoError(t, e.tdb.DB().Create(&models.RepeaterCosysop{RepeaterID: otherHotspot, UserID: 3191870, InvitedBy: 3191869, InvitedAt: now, ExpiresAt: now.Add(time.Hour), AcceptedAt: &now}).Error)

			g := e.create(t, e.owner, "North Texas", 8)
			path := fmt.Sprintf("/localgroups/%d", g.ID)
			code, msg := e.do(t, http.MethodPost, path+"/repeaters", e.owner, fmt.Sprintf(`{"repeater_id":%d}`, otherHotspot), nil)
			require.Equal(t, http.StatusOK, code, msg)

			var asks []apimodels.RepeaterLocalGroup
			code, msg = e.do(t, http.MethodGet, fmt.Sprintf("/repeaters/%d/localgroups", otherHotspot), cosysop, "", &asks)
			require.Equal(t, http.StatusOK, code, msg)
			require.Len(t, asks, 1, "a co-sysop sees the request to add the repeater")

			var list apimodels.LocalGroupList
			code, _ = e.do(t, http.MethodGet, "/localgroups", cosysop, "", &list)
			require.Equal(t, http.StatusOK, code)
			require.Len(t, list.Groups, 1, "a co-sysop sees the groups of the repeater")
			assert.True(t, list.Groups[0].Involved)

			code, msg = e.do(t, http.MethodPost, fmt.Sprintf("%s/repeaters/%d/accept", path, otherHotspot), cosysop, "", &g)
			require.Equal(t, http.StatusOK, code, msg)
			require.Len(t, g.Repeaters, 1)
			assert.Equal(t, models.LocalMemberIn, g.Repeaters[0].State)
			assert.True(t, g.Repeaters[0].Mine, "the repeater counts as the co-sysop's own")

			code, msg = e.do(t, http.MethodPatch, fmt.Sprintf("%s/repeaters/%d", path, otherHotspot), cosysop, `{"slot":1}`, &g)
			require.Equal(t, http.StatusOK, code, msg)
			code, _ = e.do(t, http.MethodPatch, path, cosysop, `{"name":"Mine now","talkgroup":8}`, nil)
			assert.Equal(t, http.StatusForbidden, code, "a co-sysop of a member does not manage the group")
			code, msg = e.do(t, http.MethodDelete, fmt.Sprintf("%s/repeaters/%d", path, otherHotspot), cosysop, "", nil)
			require.Equal(t, http.StatusOK, code, msg)

			mine := e.create(t, cosysop, "Club", 9)
			code, msg = e.do(t, http.MethodPost, fmt.Sprintf("/localgroups/%d/repeaters", mine.ID), cosysop, fmt.Sprintf(`{"repeater_id":%d}`, otherHotspot), &mine)
			require.Equal(t, http.StatusOK, code, msg)
			assert.Equal(t, models.LocalMemberIn, mine.Repeaters[0].State, "a co-sysop adds the repeater to a group of its own at once")
		})
	}
}
