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
	"fmt"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClearDeletedNetUsers migrates a database with nets and scheduled
// nets of a deleted user. They must keep only the users that still exist.
func TestClearDeletedNetUsers(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			cfg.Database.Database = fmt.Sprintf("file:memdb_%p", t)
			cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
			be.Setup(t, &cfg)
			database, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})

			const kept, deleted = uint(3191868), uint(3140001)
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610251000").Error)
			require.NoError(t, database.Exec("INSERT INTO users (id, callsign, username, password, approved) VALUES (?, ?, ?, ?, ?)", kept, "KI5VMF", "kept", "hash", true).Error)
			for id, user := range map[uint]uint{1: kept, 2: deleted} {
				require.NoError(t, database.Exec("INSERT INTO nets (id, talkgroup_id, started_by_user_id, start_time, active) VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)", id, 91, user, false).Error)
				require.NoError(t, database.Exec("INSERT INTO scheduled_nets (id, talkgroup_id, created_by_user_id, name) VALUES (?, ?, ?, ?)", id, 91, user, "weekly").Error)
			}

			require.NoError(t, migration.Migrate(database, &cfg))

			for _, ref := range []struct{ table, column string }{{"nets", "started_by_user_id"}, {"scheduled_nets", "created_by_user_id"}} {
				var rows []struct{ UserID *uint }
				require.NoError(t, database.Raw(fmt.Sprintf("SELECT %s AS user_id FROM %s ORDER BY id", ref.column, ref.table)).Scan(&rows).Error)
				require.Len(t, rows, 2, ref.table)
				require.NotNil(t, rows[0].UserID, ref.table)
				assert.Equal(t, kept, *rows[0].UserID, ref.table)
				assert.Nil(t, rows[1].UserID, ref.table)
			}
		})
	}
}
