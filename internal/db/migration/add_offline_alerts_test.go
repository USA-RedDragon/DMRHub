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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

// TestAddOfflineAlertsCreatesItsTableAndRollsBack runs the migration on
// a database without the table, uses the table, and rolls it back.
func TestAddOfflineAlertsCreatesItsTableAndRollsBack(t *testing.T) {
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

			m := database.Migrator()
			require.NoError(t, m.DropTable(&models.OfflineAlert{}))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202611011703").Error)
			require.NoError(t, migration.Migrate(database, &cfg))
			require.True(t, m.HasTable(&models.OfflineAlert{}))

			require.NoError(t, database.Create(&models.User{ID: 1000001, Callsign: "N0CALL", Username: "n0call", Password: "hash"}).Error)
			require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100000101}, OwnerID: 1000001, Password: "pw"}).Error)
			require.NoError(t, models.SetOfflineAlert(database, 1000001, 100000101, 10, true, time.Now()))

			require.NoError(t, migration.Rollback(database, &cfg, "202611011703"))
			require.False(t, m.HasTable("offline_alerts"))
		})
	}
}
