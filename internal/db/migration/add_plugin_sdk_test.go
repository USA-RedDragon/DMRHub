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

// TestAddPluginSDKAddsTheStoreAndSwitchTablesAndDropsThem migrates a
// database from before the plugin store and switches, and rolls the
// migration back.
func TestAddPluginSDKAddsTheStoreAndSwitchTablesAndDropsThem(t *testing.T) {
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

			tables := []any{&models.PluginStoreEntry{}, &models.PluginSwitch{}, &models.PluginUserSwitch{}}
			m := database.Migrator()
			require.NoError(t, m.DropTable(tables...))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202611020200").Error)

			require.NoError(t, migration.Migrate(database, &cfg))
			for _, table := range tables {
				assert.True(t, m.HasTable(table))
			}
			require.NoError(t, database.Create(&models.PluginUserSwitch{PluginID: 1, UserID: 1, Key: "aprs", On: true}).Error)
			assert.True(t, m.HasColumn(&models.PluginUserSwitch{}, "is_on"))

			require.NoError(t, migration.Rollback(database, &cfg, "202611020200"))
			for _, table := range tables {
				assert.False(t, m.HasTable(table))
			}
		})
	}
}
