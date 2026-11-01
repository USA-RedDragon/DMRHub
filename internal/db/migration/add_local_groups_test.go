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

package migration_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddLocalGroupsMigratesAndRollsBack(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
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
			m := database.Migrator()
			columns := []string{"is_to_local_group", "local_group_id", "local_group_name"}

			require.NoError(t, database.Create(&models.Call{StreamID: 1, GroupCall: true, DestinationID: 8}).Error)
			require.NoError(t, migration.ByID("202611020100").Rollback(database))
			assert.False(t, m.HasTable("local_groups"))
			assert.False(t, m.HasTable("local_group_members"))
			for _, col := range columns {
				assert.False(t, m.HasColumn(&models.Call{}, col), col)
			}

			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202611020100").Error)
			require.NoError(t, migration.Migrate(database, &cfg))
			assert.True(t, m.HasTable(&models.LocalGroup{}))
			assert.True(t, m.HasTable(&models.LocalGroupMember{}))
			for _, col := range columns {
				assert.True(t, m.HasColumn(&models.Call{}, col), col)
			}
			var calls []models.Call
			require.NoError(t, database.Find(&calls).Error)
			require.Len(t, calls, 1)
			assert.False(t, calls[0].IsToLocalGroup, "an old call is to no local group")

			g := models.LocalGroup{Name: "North Texas", Talkgroup: 8, OwnerID: 1}
			require.NoError(t, database.Create(&g).Error)
			require.NoError(t, database.Create(&models.LocalGroupMember{LocalGroupID: g.ID, RepeaterID: 311888, Talkgroup: 8, Slot: 2, State: models.LocalMemberIn}).Error)
			other := models.LocalGroup{Name: "Club", Talkgroup: 8, OwnerID: 1}
			require.NoError(t, database.Create(&other).Error)
			assert.Error(t, database.Create(&models.LocalGroupMember{LocalGroupID: other.ID, RepeaterID: 311888, Talkgroup: 8, Slot: 2, State: models.LocalMemberIn}).Error,
				"the database keeps a repeater in one group for each talkgroup")
		})
	}
}
