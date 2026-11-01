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
	"path/filepath"
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

// TestMakeDBGivesUpWhenItsContextEnds holds the migration lock of a
// database and opens it with a context. MakeDBContext waits for the lock,
// and returns the error of the context once the context ends.
func TestMakeDBGivesUpWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.MySQLBackend(), testutils.PostgresRedisBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			be.Setup(t, &cfg)
			holder, err := gorm.Open(db.Dialect(&cfg), &gorm.Config{Logger: logger.Discard})
			require.NoError(t, err)
			t.Cleanup(func() { closeDB(t, holder) })
			unlock, err := db.LockMigrations(holder, &cfg)
			require.NoError(t, err)
			defer func() { assert.NoError(t, unlock()) }()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			got := make(chan error, 1)
			go func() {
				gdb, err := db.MakeDBContext(ctx, &cfg)
				if err == nil {
					closeDB(t, gdb)
				}
				got <- err
			}()
			wait, stop := context.WithTimeout(t.Context(), db.LockWait)
			defer stop()
			for !lockWaiter(wait, t, holder, cfg.Database.Driver) {
				select {
				case err := <-got:
					require.Fail(t, "MakeDBContext returned while another session held the migration lock", "error: %v", err)
				default:
				}
				require.NoError(t, wait.Err(), "MakeDBContext did not wait for the migration lock within %s", db.LockWait)
			}
			cancel()
			select {
			case err := <-got:
				require.ErrorIs(t, err, context.Canceled)
			case <-wait.Done():
				require.Fail(t, "MakeDBContext kept waiting for the migration lock after its context ended")
			}
		})
	}
}

// TestMigrateRunsOnAfterItsContextEnds connects to each database, ends the
// context, then migrates. The migration runs to its end, and the
// migration lock is free after it.
func TestMigrateRunsOnAfterItsContextEnds(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{sqliteFile(), testutils.MySQLBackend(), testutils.PostgresRedisBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			be.Setup(t, &cfg)
			ctx, cancel := context.WithCancel(t.Context())
			c, err := db.Connect(ctx, &cfg)
			require.NoError(t, err)
			cancel()
			gdb, err := c.Migrate(ctx)
			require.NoError(t, err, "the end of the context stopped the migration")
			t.Cleanup(func() { closeDB(t, gdb) })
			require.NoError(t, db.Migrate(gdb.WithContext(t.Context()), &cfg), "the migration did not end")

			var held int64
			switch cfg.Database.Driver {
			case config.DatabaseDriverMySQL:
				require.NoError(t, gdb.Raw("SELECT 1 - IS_FREE_LOCK(LEFT(CONCAT(?, DATABASE()), 64))", db.MigrationLockName).Scan(&held).Error)
			case config.DatabaseDriverPostgres:
				require.NoError(t, gdb.Raw("SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())").Scan(&held).Error)
			case config.DatabaseDriverSQLite:
			}
			assert.Zero(t, held, "the migration lock is still held")
		})
	}
}

func sqliteFile() testutils.Backend {
	return testutils.Backend{Name: "sqlite", Setup: func(t *testing.T, cfg *config.Config) {
		t.Helper()
		cfg.Database.Database = filepath.Join(t.TempDir(), "DMRHub.db")
	}}
}
