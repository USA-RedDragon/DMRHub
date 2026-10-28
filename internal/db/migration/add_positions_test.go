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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddPositionsKeepsUsersNotSharing(t *testing.T) {
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
			require.NoError(t, m.DropTable("location_requests", &models.Position{}, &models.PositionSetting{}))
			require.NoError(t, m.DropColumn(&models.User{}, "SharePositions"))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610232100").Error)
			require.NoError(t, database.Exec(
				"INSERT INTO users (id, callsign, username, password, admin, super_admin, email, approved, suspended, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				3191868, "KI5VMF", "old", "", false, false, "", true, false, time.Now(), time.Now(),
			).Error)

			require.NoError(t, migration.Migrate(database, &cfg))

			for _, table := range []any{&models.PositionSetting{}, &models.Position{}, "location_requests"} {
				assert.True(t, m.HasTable(table))
			}
			var got models.User
			require.NoError(t, database.First(&got, 3191868).Error)
			assert.False(t, got.SharePositions)
			require.NoError(t, models.SetPositionTracking(database, 3191868, 3191868, true, time.Now()))
			require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 319186801, Callsign: "KI5VMF"}, Trusted: true}).Error)
			stored, err := models.StorePosition(database, models.Position{RadioID: 3191868, Latitude: 1, Longitude: 2, Format: models.PositionFormatNMEA, ReceivedAt: time.Now().Add(time.Second), RepeaterID: 319186801})
			require.NoError(t, err)
			assert.True(t, stored)
		})
	}
}
