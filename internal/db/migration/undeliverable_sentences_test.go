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
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type undeliverableCall struct {
	ID            uint `gorm:"primaryKey"`
	StreamID      uint
	Undeliverable []string `gorm:"serializer:json;type:text"`
}

func (undeliverableCall) TableName() string { return "calls" }

// TestUndeliverableReasonsBecomeSentences migrates calls whose
// undeliverable lists hold entries of the old form, such as
// "ipsc:314926:slot_busy". Each such entry becomes its sentence, entries
// that are sentences already stay, and calls without a list stay as they
// are.
func TestUndeliverableReasonsBecomeSentences(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			be.Setup(t, &cfg)
			database := openRaw(t, &cfg)
			require.NoError(t, migration.Migrate(database, &cfg))
			require.NoError(t, database.AutoMigrate(&undeliverableCall{}))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610261200").Error)

			busy := "IPSC repeater 314926 was busy on that slot."
			rows := map[uint][]string{
				1: {"ipsc:314926:slot_busy", "ipsc:311304200:no_ipsc_form"},
				2: {busy, "ipsc:311304201:fec_uncorrectable"},
				3: {busy},
				4: nil,
			}
			want := map[uint][]string{
				1: {busy, "IPSC repeater 311304200 cannot carry this kind of burst."},
				2: {busy, "The burst for IPSC repeater 311304201 was too damaged to repair."},
				3: {busy},
				4: nil,
			}
			for stream, list := range rows {
				require.NoError(t, database.Create(&undeliverableCall{StreamID: stream, Undeliverable: list}).Error)
			}

			require.NoError(t, migration.Migrate(database, &cfg))

			var calls []undeliverableCall
			require.NoError(t, database.Find(&calls).Error)
			require.Len(t, calls, len(rows))
			for _, c := range calls {
				assert.Equal(t, want[c.StreamID], c.Undeliverable, "stream %d", c.StreamID)
			}
			require.NoError(t, migration.Migrate(database, &cfg))
		})
	}
}
