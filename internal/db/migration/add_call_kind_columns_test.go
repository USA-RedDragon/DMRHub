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
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCallKindColumnsKeepsOldCallsAsVoice(t *testing.T) {
	t.Parallel()
	newCallColumns := []string{
		"Kind", "SubKind", "SourceID", "Transport", "IngressID", "Bursts", "FID", "Opcode", "SAP",
		"DataFormat", "Confirmed", "Blocks", "Octets", "BadCRC", "LostBlocks", "Response",
		"TalkerAlias", "AliasFormat", "Latitude", "Longitude", "PositionError", "Partial", "Undeliverable",
	}
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
			require.NoError(t, m.DropIndex(&models.Call{}, "Kind"))
			require.NoError(t, m.DropIndex(&models.Call{}, "SourceID"))
			for _, col := range newCallColumns {
				require.NoError(t, m.DropColumn(&models.Call{}, col), col)
			}
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610221910").Error)

			require.NoError(t, database.Create(&models.User{ID: 1000001, Callsign: "N0CALL", Username: "old"}).Error)
			require.NoError(t, database.Exec(
				"INSERT INTO calls (stream_id, start_time, duration, active, user_id, repeater_id, time_slot, group_call, destination_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				7, time.Now(), int64(time.Second), false, 1000001, nil, false, true, 91, time.Now(), time.Now(),
			).Error)

			require.NoError(t, migration.Migrate(database, &cfg))

			var got models.Call
			require.NoError(t, database.First(&got).Error)
			assert.Equal(t, models.CallKindVoice, got.Kind)
			assert.Equal(t, uint(1000001), got.SourceID)
			require.NotNil(t, got.UserID)
			assert.Equal(t, uint(1000001), *got.UserID)
			assert.Nil(t, got.RepeaterID)
			assert.Empty(t, got.Undeliverable)
		})
	}
}
