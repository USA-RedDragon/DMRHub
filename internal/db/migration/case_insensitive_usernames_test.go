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
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func caseUser(id uint, username string) *models.User {
	return &models.User{ID: id, Callsign: fmt.Sprintf("N%dCALL", id), Username: username, Password: "hash"}
}

// TestCaseInsensitiveUsernames makes a database, where two usernames that
// differ only by case cannot both exist. On a database of before the
// migration with such usernames, the migration refuses and names each
// group, and runs once an admin renamed them.
func TestCaseInsensitiveUsernames(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			memdb.Use(&cfg)
			be.Setup(t, &cfg)
			database, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})

			require.NoError(t, database.Create(caseUser(3000001, "Alice")).Error)
			require.Error(t, database.Create(caseUser(3000002, "alice")).Error, "a username in another case is taken")
			require.Error(t, database.Create(caseUser(3000002, "ALICE")).Error, "a username in another case is taken")
			require.NoError(t, database.Create(caseUser(3000002, "alice2")).Error)
			taken, err := models.UsernameTaken(database, "aLiCe", 0)
			require.NoError(t, err)
			assert.True(t, taken)
			taken, err = models.UsernameTaken(database, "aLiCe", 3000001)
			require.NoError(t, err)
			assert.False(t, taken, "a user may change the case of their own username")

			undo(t, database)
			for id, name := range map[uint]string{3000003: "Bob", 3000004: "BOB", 3000005: "bob", 3000006: "carol", 3000007: "Carol"} {
				require.NoError(t, database.Create(caseUser(id, name)).Error)
			}
			err = migration.MigrateSchema(database, &cfg)
			require.ErrorIs(t, err, migration.ErrUsernameClash)
			assert.Contains(t, err.Error(), `"Bob" (user 3000003), "BOB" (user 3000004), "bob" (user 3000005)`)
			assert.Contains(t, err.Error(), `"carol" (user 3000006), "Carol" (user 3000007)`)
			assert.NotContains(t, err.Error(), "Alice")
			assert.False(t, database.Migrator().HasIndex("users", "idx_users_username_ci"), "a refused migration changes nothing")
			_, err = db.MakeDB(&cfg)
			require.ErrorIs(t, err, migration.ErrUsernameClash, "DMRHub does not start with the clash")

			require.NoError(t, database.Model(&models.User{}).Where("id = ?", 3000004).Update("username", "bob2").Error)
			require.NoError(t, database.Unscoped().Delete(&models.User{}, 3000005).Error)
			require.NoError(t, database.Model(&models.User{}).Where("id = ?", 3000007).Update("username", "carol2").Error)
			require.NoError(t, migration.MigrateSchema(database, &cfg))
			require.Error(t, database.Create(caseUser(3000008, "BOB")).Error)
			require.NoError(t, database.Create(caseUser(3000008, "dave")).Error)
		})
	}
}

func undo(t *testing.T, database *gorm.DB) {
	t.Helper()
	m := database.Migrator()
	require.NoError(t, m.DropIndex("users", "idx_users_username_ci"))
	if database.Name() == "mysql" {
		require.NoError(t, m.DropColumn("users", "username_ci"))
	}
	require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610270200").Error)
}
