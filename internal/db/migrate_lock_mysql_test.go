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
	"testing"
	"time"

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
	for _, be := range []testutils.Backend{testutils.MySQLBackend(), testutils.PostgresRedisBackend()} {
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
			select {
			case unlock := <-got:
				assert.NoError(t, unlock())
				require.NoError(t, unlockFirst())
				require.Fail(t, "the second replica took the migration lock while the first held it")
			case <-time.After(2 * time.Second):
			}
			require.NoError(t, unlockFirst())
			select {
			case unlock := <-got:
				require.NoError(t, unlock())
			case <-time.After(30 * time.Second):
				require.Fail(t, "the second replica never took the migration lock after the first let it go")
			}
		})
	}
}
