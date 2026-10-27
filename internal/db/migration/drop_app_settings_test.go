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
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
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
		d = mysql.Open(fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=True&sql_mode=%%27STRICT_TRANS_TABLES%%27", cfg.Database.Username, cfg.Database.Password, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database))
	case config.DatabaseDriverPostgres:
		d = postgres.Open(fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", cfg.Database.Host, cfg.Database.Port, cfg.Database.Username, cfg.Database.Password, cfg.Database.Database))
	case config.DatabaseDriverSQLite:
		d = sqlite.Open(memdb.DSN())
	default:
		d = sqlite.Open(memdb.DSN())
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

// TestDropAppSettings migrates a database that still has the app settings
// table, with the has_seeded flag and a row, and one that never had it.
// The table must go in both, and a second run must do nothing.
func TestDropAppSettings(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		for _, had := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/had=%v", be.Name, had), func(t *testing.T) {
				t.Parallel()
				cfg, err := configulator.New(config.ConfigSchema()).Default()
				require.NoError(t, err)
				be.Setup(t, &cfg)
				database := openRaw(t, &cfg)
				if had {
					require.NoError(t, database.AutoMigrate(&seededAppSettings{}))
					require.NoError(t, database.Create(&seededAppSettings{HasSeeded: true}).Error)
				}

				require.NoError(t, migration.Migrate(database, &cfg))
				assert.False(t, database.Migrator().HasTable("app_settings"), "the table is dropped")

				require.NoError(t, migration.Migrate(database, &cfg))
				assert.False(t, database.Migrator().HasTable("app_settings"))
			})
		}
	}
}
