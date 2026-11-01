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

package models_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func localGroupBackends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

func localGroupDB(t *testing.T, be testutils.Backend) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	be.Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	for _, u := range []models.User{{ID: 3191868, Callsign: "KI5VMF", Username: "ki5vmf"}, {ID: 3191869, Callsign: "W5DFW", Username: "w5dfw"}} {
		require.NoError(t, database.Create(&u).Error)
	}
	for _, r := range []struct{ id, owner uint }{{311888, 3191868}, {311889, 3191868}, {319186901, 3191869}} {
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: r.id}, OwnerID: r.owner}).Error)
	}
	return database
}

func TestARepeaterIsInOneLocalGroupForEachTalkgroup(t *testing.T) {
	t.Parallel()
	for _, be := range localGroupBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			database := localGroupDB(t, be)
			north := models.LocalGroup{Name: "North Texas", Talkgroup: 8, OwnerID: 3191868}
			club := models.LocalGroup{Name: "Club", Talkgroup: 8, OwnerID: 3191869}
			regional := models.LocalGroup{Name: "Regional", Talkgroup: 9, OwnerID: 3191869}
			for _, g := range []*models.LocalGroup{&north, &club, &regional} {
				require.NoError(t, database.Create(g).Error)
			}

			require.NoError(t, models.AddLocalGroupMember(database, models.LocalGroupMember{LocalGroupID: north.ID, RepeaterID: 311888, Slot: 2, State: models.LocalMemberIn}))
			require.ErrorIs(t, models.AddLocalGroupMember(database, models.LocalGroupMember{LocalGroupID: north.ID, RepeaterID: 311888, Slot: 1, State: models.LocalMemberIn}), gorm.ErrDuplicatedKey)
			require.ErrorIs(t, models.AddLocalGroupMember(database, models.LocalGroupMember{LocalGroupID: club.ID, RepeaterID: 311888, Slot: 2, State: models.LocalMemberAsked}), models.ErrLocalNumberTaken,
				"a repeater is in one group for each local talkgroup")
			require.NoError(t, models.AddLocalGroupMember(database, models.LocalGroupMember{LocalGroupID: regional.ID, RepeaterID: 311888, Slot: 1, State: models.LocalMemberAsked}),
				"a repeater may be in groups on other local talkgroups")

			rpt, err := models.FindRepeaterByID(database, 311888)
			require.NoError(t, err)
			assert.Equal(t, []models.LocalMembership{{GroupID: north.ID, Name: "North Texas", Talkgroup: 8, Slot: true}}, rpt.LocalGroups,
				"a repeater only asked to join a group takes none of its calls")

			require.NoError(t, models.SetLocalGroupMember(database, regional.ID, 311888, 0, true))
			require.NoError(t, models.SetLocalGroupMember(database, regional.ID, 311888, 1, true), "a change to the values a repeater has is no error")
			require.NoError(t, models.UpdateLocalGroup(database, regional.ID, "Regional", 9), "a save with no change is no error")
			require.ErrorIs(t, models.SetLocalGroupMember(database, regional.ID, 311889, 1, true), gorm.ErrRecordNotFound)
			require.ErrorIs(t, models.UpdateLocalGroup(database, regional.ID+100, "Gone", 9), gorm.ErrRecordNotFound)
			rpt, err = models.FindRepeaterByID(database, 311888)
			require.NoError(t, err)
			assert.Equal(t, []models.LocalMembership{
				{GroupID: north.ID, Name: "North Texas", Talkgroup: 8, Slot: true},
				{GroupID: regional.ID, Name: "Regional", Talkgroup: 9, Slot: false},
			}, rpt.LocalGroups)

			require.NoError(t, models.AddLocalGroupMember(database, models.LocalGroupMember{LocalGroupID: club.ID, RepeaterID: 319186901, Slot: 2, State: models.LocalMemberIn}))
			require.NoError(t, models.AddLocalGroupMember(database, models.LocalGroupMember{LocalGroupID: club.ID, RepeaterID: 311889, Slot: 2, State: models.LocalMemberIn}))
			require.ErrorIs(t, models.UpdateLocalGroup(database, regional.ID, "Regional", 8), models.ErrLocalNumberTaken,
				"a group cannot move to a number that one of its repeaters has another group on")
			require.NoError(t, models.UpdateLocalGroup(database, club.ID, "Club hotspots", 9))
			ids, err := models.LocalGroupRepeaterIDs(database, club.ID)
			require.NoError(t, err)
			assert.Equal(t, []uint{311889, 319186901}, ids)
		})
	}
}

func TestDeletingARepeaterOrItsOwnerEndsItsLocalGroups(t *testing.T) {
	t.Parallel()
	for _, be := range localGroupBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			database := localGroupDB(t, be)
			mine := models.LocalGroup{Name: "Mine", Talkgroup: 8, OwnerID: 3191868}
			theirs := models.LocalGroup{Name: "Theirs", Talkgroup: 9, OwnerID: 3191869}
			require.NoError(t, database.Create(&mine).Error)
			require.NoError(t, database.Create(&theirs).Error)
			for _, m := range []models.LocalGroupMember{
				{LocalGroupID: mine.ID, RepeaterID: 311888, Slot: 2, State: models.LocalMemberIn},
				{LocalGroupID: mine.ID, RepeaterID: 319186901, Slot: 2, State: models.LocalMemberIn},
				{LocalGroupID: theirs.ID, RepeaterID: 311889, Slot: 1, State: models.LocalMemberIn},
				{LocalGroupID: theirs.ID, RepeaterID: 319186901, Slot: 1, State: models.LocalMemberIn},
			} {
				require.NoError(t, models.AddLocalGroupMember(database, m))
			}

			require.NoError(t, models.DeleteRepeater(database, 311889))
			ids, err := models.LocalGroupRepeaterIDs(database, theirs.ID)
			require.NoError(t, err)
			assert.Equal(t, []uint{319186901}, ids)

			require.NoError(t, models.DeleteUser(database, 3191868))
			var n int64
			require.NoError(t, database.Model(&models.LocalGroup{}).Where("id = ?", mine.ID).Count(&n).Error)
			assert.Zero(t, n, "the groups of a deleted user go")
			require.NoError(t, database.Model(&models.LocalGroupMember{}).Where("local_group_id = ? OR repeater_id = ?", mine.ID, 311888).Count(&n).Error)
			assert.Zero(t, n, "the rows of the groups and repeaters of a deleted user go")
			ids, err = models.LocalGroupRepeaterIDs(database, theirs.ID)
			require.NoError(t, err)
			assert.Equal(t, []uint{319186901}, ids, "the groups of other users keep their other repeaters")

			members, err := models.DeleteLocalGroup(database, theirs.ID)
			require.NoError(t, err)
			assert.Equal(t, []uint{319186901}, members)
			rpt, err := models.FindRepeaterByID(database, 319186901)
			require.NoError(t, err)
			assert.Empty(t, rpt.LocalGroups)
		})
	}
}
