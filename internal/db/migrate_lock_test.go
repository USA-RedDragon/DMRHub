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
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func closeDB(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	sqlDB, err := gdb.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func TestMakeDBConcurrentMigrationsOnPostgres(t *testing.T) {
	t.Parallel()

	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	testutils.PostgresRedisBackend().Setup(t, &cfg)

	admin, err := sql.Open("pgx", fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		cfg.Database.Username, cfg.Database.Password,
		net.JoinHostPort(cfg.Database.Host, strconv.Itoa(cfg.Database.Port)), cfg.Database.Database))
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })

	cases := []struct {
		name    string
		prepare func(t *testing.T, cfg *config.Config)
	}{
		{name: "fresh", prepare: func(*testing.T, *config.Config) {}},
		{name: "pending", prepare: func(t *testing.T, cfg *config.Config) {
			gdb, err := db.MakeDB(cfg)
			require.NoError(t, err)
			require.NoError(t, gdb.Exec("DELETE FROM migrations WHERE id IN ?", []string{"202610242000", "202610242100"}).Error)
			closeDB(t, gdb)
		}},
	}

	const rounds = 8
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for round := range rounds {
				name := fmt.Sprintf("%s_%d", tc.name, round)
				_, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+name)
				require.NoError(t, err)

				roundCfg := cfg
				roundCfg.Database.Database = name
				tc.prepare(t, &roundCfg)

				var wg sync.WaitGroup
				start := make(chan struct{})
				errs := make([]error, 2)
				for i := range errs {
					wg.Go(func() {
						<-start
						gdb, err := db.MakeDB(&roundCfg)
						errs[i] = err
						if gdb != nil {
							if sqlDB, err := gdb.DB(); err == nil {
								_ = sqlDB.Close()
							}
						}
					})
				}
				close(start)
				wg.Wait()
				for i, err := range errs {
					require.NoError(t, err, "%s run %d", name, i)
				}

				gdb, err := db.MakeDB(&roundCfg)
				require.NoError(t, err)
				var latest int64
				require.NoError(t, gdb.Table("migrations").Where("id = ?", "202610261100").Count(&latest).Error)
				require.Equal(t, int64(1), latest, name)
				closeDB(t, gdb)
			}
		})
	}
}
