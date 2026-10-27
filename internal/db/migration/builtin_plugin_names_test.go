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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnOldHubLetsAnExternalPluginTakeTheNameOfABuiltinOne(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			database, cfg := builtinsDB(t, be)
			m := database.Migrator()
			if m.HasIndex(&models.Plugin{}, "idx_plugins_builtin_name") {
				require.NoError(t, m.DropIndex(&models.Plugin{}, "idx_plugins_builtin_name"))
			}
			if !m.HasIndex(&models.Plugin{}, "idx_plugins_name") {
				require.NoError(t, database.Exec("CREATE UNIQUE INDEX idx_plugins_name ON plugins (name)").Error)
			}
			_, err := models.EnsureBuiltinPlugin(database, "parrot", "Parrot", "v1.3.4", []models.PluginRange{{First: 9990, Last: 9990}}, time.Now())
			require.NoError(t, err)

			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610261300").Error)
			require.NoError(t, migration.Migrate(database, cfg))
			require.NoError(t, database.AutoMigrate(&models.Plugin{}))

			assert.False(t, m.HasIndex(&models.Plugin{}, "idx_plugins_name"))
			assert.True(t, m.HasIndex(&models.Plugin{}, "idx_plugins_builtin_name"))
			_, _, err = models.CreatePlugin(database, "Parrot", []models.PluginRange{{First: 9990, Last: 9990}}, time.Now())
			require.NoError(t, err)
			require.Error(t, database.Create(&models.Plugin{Name: "Parrot", TokenHash: []byte("another token"), TokenCreatedAt: time.Now()}).Error,
				"two plugins that are not built in never share a name")
		})
	}
}
