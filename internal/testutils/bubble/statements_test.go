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

package bubble_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	_ "github.com/glebarez/go-sqlite"
	"github.com/stretchr/testify/require"
)

// TestSettleDBWaitsForAStatementThatWaitsForALock writes a row while
// another connection holds the write lock of the SQLite file, so that the
// write waits in the busy handler of SQLite, whose sleeps durably block a
// goroutine on the virtual clock. synctest.Wait returns meanwhile; after
// the lock goes, SettleDB must last until the write is done.
func TestSettleDBWaitsForAStatementThatWaitsForALock(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		memdb.Use(t, &cfg)
		database, err := db.MakeDB(&cfg)
		require.NoError(t, err)
		t.Cleanup(func() {
			if sqlDB, err := database.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		_, err = bubble.CountStatements(database)
		require.NoError(t, err)
		t.Cleanup(func() { bubble.Forget(database) })

		other, err := sql.Open("sqlite", cfg.Database.Database)
		require.NoError(t, err)
		t.Cleanup(func() { _ = other.Close() })
		conn, err := other.Conn(context.Background())
		require.NoError(t, err)
		_, err = conn.ExecContext(context.Background(), "BEGIN IMMEDIATE")
		require.NoError(t, err)

		var written atomic.Bool
		go func() {
			if database.Create(&models.Talkgroup{ID: 7, Name: "TG7"}).Error == nil {
				written.Store(true)
			}
		}()
		synctest.Wait()
		require.False(t, written.Load(), "the write did not wait for the lock")

		_, err = conn.ExecContext(context.Background(), "COMMIT")
		require.NoError(t, err)
		require.NoError(t, conn.Close())
		bubble.SettleDB(t, database, time.Minute)
		require.True(t, written.Load(), "SettleDB returned before the write that waited for the lock")
	})
}
