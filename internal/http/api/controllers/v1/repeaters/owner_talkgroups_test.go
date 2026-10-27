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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAnOwnerSetsTheTalkgroupsOfTheirRepeater sets the talkgroups of a
// repeater as its owner, on the plain test router.
func TestAnOwnerSetsTheTalkgroupsOfTheirRepeater(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "owner", Password: "password"}
	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
	repeaterPost := apimodels.RepeaterPost{RadioID: 319186807, Type: "mmdvm"}
	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	require.Equal(t, http.StatusOK, w.Code)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	_, w = testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{ID: 50001, Name: "Test TG", Description: "Test talkgroup"})
	require.Equal(t, http.StatusOK, w.Code)

	resp, w := testutils.SetRepeaterTalkgroups(t, router, repeaterPost.RadioID, userJar, apimodels.RepeaterTalkgroupsPost{
		TS1StaticTalkgroups: []models.Talkgroup{{ID: 50001}},
	})
	assert.Equal(t, http.StatusOK, w.Code, resp.Error)
	assert.Equal(t, "Repeater talkgroups updated", resp.Message)

	rpt, err := models.FindRepeaterByID(tdb.DB(), repeaterPost.RadioID)
	require.NoError(t, err)
	require.Len(t, rpt.TS1StaticTalkgroups, 1)
	assert.Equal(t, uint(50001), rpt.TS1StaticTalkgroups[0].ID)
}

func postRawTalkgroups(t *testing.T, router *gin.Engine, id uint, jar testutils.CookieJar, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, fmt.Sprintf("/api/v1/repeaters/%d/talkgroups", id), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)
	return w
}

func dynamicIDs(t *testing.T, db *gorm.DB, id uint) (*uint, *uint) {
	t.Helper()
	rpt, err := models.FindRepeaterByID(db, id)
	require.NoError(t, err)
	return rpt.TS1DynamicTalkgroupID, rpt.TS2DynamicTalkgroupID
}

func TestADynamicTalkgroupLeftOutOfTheBodyStays(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "owner", Password: "password"}
	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
	repeaterPost := apimodels.RepeaterPost{RadioID: 319186807, Type: "mmdvm"}
	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	require.Equal(t, http.StatusOK, w.Code)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	for _, tg := range []uint{50001, 50002} {
		_, w = testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{ID: tg, Name: fmt.Sprintf("TG %d", tg), Description: "Test talkgroup"})
		require.Equal(t, http.StatusOK, w.Code)
	}

	w = postRawTalkgroups(t, router, repeaterPost.RadioID, userJar,
		`{"ts1_static_talkgroups":[],"ts2_static_talkgroups":[],"ts1_dynamic_talkgroup":{"id":50001},"ts2_dynamic_talkgroup":{"id":50002}}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ts1, ts2 := dynamicIDs(t, tdb.DB(), repeaterPost.RadioID)
	require.NotNil(t, ts1)
	require.NotNil(t, ts2)

	w = postRawTalkgroups(t, router, repeaterPost.RadioID, userJar, `{"ts1_static_talkgroups":[{"id":50001}],"ts2_static_talkgroups":[]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ts1, ts2 = dynamicIDs(t, tdb.DB(), repeaterPost.RadioID)
	require.NotNil(t, ts1, "a left out dynamic talkgroup stays")
	assert.Equal(t, uint(50001), *ts1)
	require.NotNil(t, ts2, "a left out dynamic talkgroup stays")
	assert.Equal(t, uint(50002), *ts2)

	w = postRawTalkgroups(t, router, repeaterPost.RadioID, userJar, `{"ts1_static_talkgroups":[],"ts2_static_talkgroups":[],"ts1_dynamic_talkgroup":null}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ts1, ts2 = dynamicIDs(t, tdb.DB(), repeaterPost.RadioID)
	assert.Nil(t, ts1, "null clears it")
	require.NotNil(t, ts2)

	w = postRawTalkgroups(t, router, repeaterPost.RadioID, userJar, `{"ts1_static_talkgroups":[],"ts2_static_talkgroups":[],"ts2_dynamic_talkgroup":{"id":0}}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, ts2 = dynamicIDs(t, tdb.DB(), repeaterPost.RadioID)
	assert.Nil(t, ts2, "the ID 0 clears it")
}
