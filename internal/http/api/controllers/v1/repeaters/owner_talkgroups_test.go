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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
