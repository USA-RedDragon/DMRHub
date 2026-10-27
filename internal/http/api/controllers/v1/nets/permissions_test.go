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

package nets_test

import (
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnlyTalkgroupAdminsNCOsAndAdminsRunNets(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)
	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{Name: "Perm TG", Description: "nets"})
	require.Equal(t, http.StatusOK, tgW.Code)
	var tg models.Talkgroup
	require.NoError(t, tdb.DB().Where("name = ?", "Perm TG").First(&tg).Error)

	plain := testutils.SeedAccount(t, tdb.DB(), 3100001, "N0CALL", "plain")
	nco := testutils.SeedAccount(t, tdb.DB(), 3100002, "N0NCO", "nco")
	owner := testutils.SeedAccount(t, tdb.DB(), 3100003, "N0OWN", "owner")
	require.NoError(t, tdb.DB().Model(&tg).Association("NCOs").Append(&nco))
	require.NoError(t, tdb.DB().Model(&tg).Association("Admins").Append(&owner))

	schedule := func(name string) apimodels.ScheduledNetPost {
		return apimodels.ScheduledNetPost{TalkgroupID: tg.ID, Name: name, DayOfWeek: 3, TimeOfDay: "19:00", Timezone: "UTC"}
	}

	plainJar := testutils.Login(t, router, plain.Username)
	_, w := testutils.StartNet(t, router, plainJar, apimodels.NetStartPost{TalkgroupID: tg.ID})
	assert.Equal(t, http.StatusForbidden, w.Code, "a user who is not an NCO or admin of the talkgroup starts a net")
	_, w = testutils.CreateScheduledNet(t, router, plainJar, schedule("Plain"))
	assert.Equal(t, http.StatusForbidden, w.Code, "a user who is not an NCO or admin of the talkgroup schedules a net")

	for _, u := range []models.User{nco, owner} {
		jar := testutils.Login(t, router, u.Username)
		net, w := testutils.StartNet(t, router, jar, apimodels.NetStartPost{TalkgroupID: tg.ID})
		require.Equal(t, http.StatusCreated, w.Code, "%s starts a net", u.Username)
		_, w = testutils.StopNet(t, router, jar, net.ID)
		require.Equal(t, http.StatusOK, w.Code, "%s stops the net", u.Username)
		_, w = testutils.CreateScheduledNet(t, router, jar, schedule(u.Username))
		assert.Equal(t, http.StatusCreated, w.Code, "%s schedules a net", u.Username)
	}

	_, w = testutils.StartNet(t, router, adminJar, apimodels.NetStartPost{TalkgroupID: tg.ID})
	assert.Equal(t, http.StatusCreated, w.Code, "an admin starts a net")
}
