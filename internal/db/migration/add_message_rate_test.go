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

// TestAddMessageRateLeavesOldMessagesWithoutARate migrates a database from
// before the hub kept the rate of a message. Its messages must come out
// with rate 0, which no stored message has, and without a P_HEAD.
func TestAddMessageRateLeavesOldMessagesWithoutARate(t *testing.T) {
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
			require.NoError(t, m.DropColumn(&models.Message{}, "Rate"))
			require.NoError(t, m.DropColumn(&models.Message{}, "Proprietary"))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202611011800").Error)
			require.NoError(t, database.Exec(
				"INSERT INTO messages (received_at, source_id, destination_id, format, checksum_ok) VALUES (?, ?, ?, ?, ?)",
				time.Now().UTC(), 3140598, 3191868, models.MessageFormatAnyTone, true,
			).Error)

			require.NoError(t, migration.Migrate(database, &cfg))

			var got models.Message
			require.NoError(t, database.First(&got).Error)
			assert.Equal(t, uint8(0), got.Rate)
			assert.False(t, got.Proprietary)
			require.NoError(t, database.Create(&models.Message{ReceivedAt: time.Now(), SourceID: 1, DestinationID: 2, Format: models.MessageFormatTMS, Rate: 8, Proprietary: true}).Error)
		})
	}
}
