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
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type seededAppSettings struct {
	ID        uint `gorm:"primaryKey"`
	HasSeeded bool
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (seededAppSettings) TableName() string { return "app_settings" }

func openRaw(t *testing.T, cfg *config.Config) *gorm.DB {
	t.Helper()
	var d gorm.Dialector
	switch cfg.Database.Driver {
	case config.DatabaseDriverMySQL:
		d = mysql.Open(fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=True", cfg.Database.Username, cfg.Database.Password, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database))
	case config.DatabaseDriverPostgres:
		d = postgres.Open(fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", cfg.Database.Host, cfg.Database.Port, cfg.Database.Username, cfg.Database.Password, cfg.Database.Database))
	case config.DatabaseDriverSQLite:
		d = sqlite.Open(fmt.Sprintf("file:memdb_seeded_%p?mode=memory&cache=shared", t))
	default:
		d = sqlite.Open(fmt.Sprintf("file:memdb_seeded_%p?mode=memory&cache=shared", t))
	}
	gdb, err := gorm.Open(d, &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

// TestDropAppSettingsHasSeeded migrates a database whose app settings have
// the has_seeded flag that seeded nothing any more. The column must go and
// the row must stay.
func TestDropAppSettingsHasSeeded(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			be.Setup(t, &cfg)
			database := openRaw(t, &cfg)
			require.NoError(t, database.AutoMigrate(&seededAppSettings{}))
			require.NoError(t, database.Create(&seededAppSettings{HasSeeded: true}).Error)

			require.NoError(t, migration.Migrate(database, &cfg))

			m := database.Migrator()
			assert.False(t, m.HasColumn("app_settings", "has_seeded"), "the column is dropped")
			var rows int64
			require.NoError(t, database.Table("app_settings").Count(&rows).Error)
			assert.Equal(t, int64(1), rows, "the row stays")

			require.NoError(t, migration.Migrate(database, &cfg))
		})
	}
}
