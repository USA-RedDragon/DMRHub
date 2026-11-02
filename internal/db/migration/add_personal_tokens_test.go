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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAddPersonalTokensCreatesItsTableAndRollsBack runs the migration on
// a database that AutoMigrate gave the table, where the rows stay, and on
// a database without the table, where it makes the columns that
// AutoMigrate makes, uses the table, and rolls it back.
func TestAddPersonalTokensCreatesItsTableAndRollsBack(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			memdb.UseEmpty(t, &cfg)
			be.Setup(t, &cfg)
			database, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})

			m := database.Migrator()
			require.NoError(t, m.DropTable(&models.PersonalToken{}))
			require.NoError(t, database.AutoMigrate(&models.PersonalToken{}))
			autoColumns := columnsOf(t, database, "personal_tokens")
			expires := time.Now().Add(24 * time.Hour)
			token := models.PersonalToken{ID: "tok", UserID: 1000001, Name: "script", Prefix: "dmrhub_ab", Hash: "h", Scopes: "read", Days: 30, CreatedAt: time.Now(), ExpiresAt: &expires}
			require.NoError(t, database.Create(&token).Error)
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202611011647").Error)
			require.NoError(t, migration.Migrate(database, &cfg))
			var kept models.PersonalToken
			require.NoError(t, database.First(&kept, "id = ?", "tok").Error)
			require.Equal(t, "script", kept.Name)

			require.NoError(t, m.DropTable(&models.PersonalToken{}))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202611011647").Error)
			require.NoError(t, migration.Migrate(database, &cfg))
			require.True(t, m.HasTable(&models.PersonalToken{}))
			require.Equal(t, autoColumns, columnsOf(t, database, "personal_tokens"))

			require.NoError(t, database.Create(&token).Error)
			var got models.PersonalToken
			require.NoError(t, database.First(&got, "id = ?", "tok").Error)
			require.Equal(t, "script", got.Name)

			require.NoError(t, migration.Rollback(database, &cfg, "202611011647"))
			require.False(t, m.HasTable("personal_tokens"))
		})
	}
}

func columnsOf(t *testing.T, database *gorm.DB, table string) []string {
	t.Helper()
	types, err := database.Migrator().ColumnTypes(table)
	require.NoError(t, err)
	out := make([]string, 0, len(types))
	for _, c := range types {
		nullable, _ := c.Nullable()
		length, _ := c.Length()
		pk, _ := c.PrimaryKey()
		unique, _ := c.Unique()
		out = append(out, fmt.Sprintf("%s %s %d null=%t pk=%t unique=%t", c.Name(), c.DatabaseTypeName(), length, nullable, pk, unique))
	}
	return out
}
