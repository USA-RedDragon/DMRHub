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
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddRepeaterDialectColumnKeepsRepeaters(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
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

			m := database.Migrator()
			require.NoError(t, m.DropColumn(&models.Repeater{}, "Dialect"))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610231200").Error)
			require.NoError(t, database.Exec(
				"INSERT INTO repeaters (id, callsign, type, password) VALUES (?, ?, ?, ?)", 311860, "N0CALL", models.RepeaterTypeMMDVM, "s3cret",
			).Error)

			require.NoError(t, migration.Migrate(database, &cfg))

			require.True(t, m.HasColumn(&models.Repeater{}, "Dialect"))
			var got models.Repeater
			require.NoError(t, database.First(&got, 311860).Error)
			assert.Equal(t, "N0CALL", got.Callsign)
			assert.Empty(t, got.Dialect)
		})
	}
}
