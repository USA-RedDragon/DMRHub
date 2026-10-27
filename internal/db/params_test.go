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

package db_test

import (
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
)

func TestServerDatabasesLeaveOutSQLitePragmas(t *testing.T) {
	t.Parallel()
	for _, driver := range []config.DatabaseDriver{config.DatabaseDriverPostgres, config.DatabaseDriverMySQL} {
		for _, extra := range [][]string{nil, {"sslmode=disable"}} {
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			cfg.Database.Driver = driver
			cfg.Database.Host = "db"
			cfg.Database.Port = 5432
			cfg.Database.Database = "dmrhub"
			cfg.Database.ExtraParameters = append(cfg.Database.ExtraParameters, extra...)
			var dsn string
			switch d := db.Dialect(&cfg).(type) {
			case *postgres.Dialector:
				dsn = d.DSN
			case *mysql.Dialector:
				dsn = d.DSN
			default:
				t.Fatalf("driver %s gave dialector %T", driver, d)
			}
			assert.NotContains(t, dsn, "_pragma", "driver %s", driver)
			for _, p := range extra {
				assert.True(t, strings.Contains(dsn, p), "driver %s left out %s: %s", driver, p, dsn)
			}
		}
	}
}
