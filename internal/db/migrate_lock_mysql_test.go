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
	"context"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMigrationLockHoldsOffASecondReplica takes the migration lock as one
// replica and then as another on its own connection pool. The second must
// wait until the first lets it go.
func TestMigrationLockHoldsOffASecondReplica(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.MySQLBackend(), testutils.PostgresRedisBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			be.Setup(t, &cfg)
			open := func() *gorm.DB {
				gdb, err := gorm.Open(db.Dialect(&cfg), &gorm.Config{Logger: logger.Discard})
				require.NoError(t, err)
				t.Cleanup(func() { closeDB(t, gdb) })
				return gdb
			}
			first, second := open(), open()

			unlockFirst, err := db.LockMigrations(first, &cfg)
			require.NoError(t, err)
			got := make(chan func() error, 1)
			go func() {
				unlock, err := db.LockMigrations(second, &cfg)
				if err != nil {
					t.Errorf("second lock: %v", err)
					unlock = func() error { return nil }
				}
				got <- unlock
			}()
			ctx, cancel := context.WithTimeout(t.Context(), db.LockWait)
			defer cancel()
			for !lockWaiter(ctx, t, first, cfg.Database.Driver) {
				select {
				case unlock := <-got:
					assert.NoError(t, unlock())
					require.NoError(t, unlockFirst())
					require.Fail(t, "the second replica took the migration lock while the first held it")
				default:
				}
				require.NoError(t, ctx.Err(), "the second replica did not wait for the migration lock within %s", db.LockWait)
			}
			select {
			case unlock := <-got:
				assert.NoError(t, unlock())
				require.NoError(t, unlockFirst())
				require.Fail(t, "the second replica took the migration lock while the first held it")
			default:
			}
			require.NoError(t, unlockFirst())
			select {
			case unlock := <-got:
				require.NoError(t, unlock())
			case <-ctx.Done():
				require.Fail(t, "the second replica never took the migration lock after the first let it go")
			}
		})
	}
}

// TestMigrationLocksOfTwoDatabasesAreApart takes the migration lock of one
// database and then of another on the same server. Neither waits for the
// other, as with Postgres.
func TestMigrationLocksOfTwoDatabasesAreApart(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			var unlocks []func() error
			for range 2 {
				cfg, err := configulator.New(config.ConfigSchema()).Default()
				require.NoError(t, err)
				be.Setup(t, &cfg)
				gdb, err := gorm.Open(db.Dialect(&cfg), &gorm.Config{Logger: logger.Discard})
				require.NoError(t, err)
				t.Cleanup(func() { closeDB(t, gdb) })
				got := make(chan func() error, 1)
				go func() {
					unlock, err := db.LockMigrations(gdb, &cfg)
					if err != nil {
						t.Errorf("lock %s: %v", cfg.Database.Database, err)
						unlock = func() error { return nil }
					}
					got <- unlock
				}()
				ctx, cancel := context.WithTimeout(t.Context(), db.LockWait)
				select {
				case unlock := <-got:
					cancel()
					unlocks = append(unlocks, unlock)
				case <-ctx.Done():
					cancel()
					for _, unlock := range unlocks {
						assert.NoError(t, unlock())
					}
					require.Fail(t, "the lock of one database waited for the lock of another")
				}
			}
			for _, unlock := range unlocks {
				require.NoError(t, unlock())
			}
		})
	}
}

func lockWaiter(ctx context.Context, t *testing.T, gdb *gorm.DB, driver config.DatabaseDriver) bool {
	t.Helper()
	query := "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE DB = DATABASE() AND INFO LIKE 'SELECT GET_LOCK%'"
	if driver == config.DatabaseDriverPostgres {
		query = "SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())"
	}
	var n int64
	err := gdb.WithContext(ctx).Raw(query).Scan(&n).Error
	if ctx.Err() == nil {
		require.NoError(t, err, "read the waiters for the migration lock")
	}
	return n > 0
}
