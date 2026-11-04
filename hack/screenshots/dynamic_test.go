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

package main

import (
	"fmt"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func dynamicDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "hub.db")), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Exec("CREATE TABLE repeaters (id integer PRIMARY KEY, ts1_dynamic_talkgroup_id integer, ts2_dynamic_talkgroup_id integer, deleted_at datetime)").Error)
	return db
}

// TestAJumpPastTheDynamicTimeoutWaitsForTheUnlink plays a group call on a
// hotspot and moves the demo clock past the dynamic talkgroup timeout.
// DMRHub unlinks the talkgroup on its next sweep, a real-time timer, so
// the wait must end only once the database shows the link gone.
func TestAJumpPastTheDynamicTimeoutWaitsForTheUnlink(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := dynamicDB(t)
		require.NoError(t, db.Exec("INSERT INTO repeaters (id, ts2_dynamic_talkgroup_id) VALUES (300000201, 3100)").Error)
		start := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
		r := newRunner(options{}, nil, nil, "http://hub", start)
		r.heardGroup(300000201, true)
		r.cursor = start.Add(dynamicTimeout + shotStep)
		go func() {
			time.Sleep(3 * time.Second)
			_ = db.Exec("UPDATE repeaters SET ts2_dynamic_talkgroup_id = NULL WHERE id = 300000201").Error
		}()
		began := time.Now()
		require.NoError(t, r.waitDynamic(t.Context(), db))
		assert.GreaterOrEqual(t, time.Since(began), 3*time.Second)
		var left int64
		require.NoError(t, db.Raw("SELECT count(*) FROM repeaters WHERE ts2_dynamic_talkgroup_id IS NOT NULL").Scan(&left).Error)
		assert.Zero(t, left)
	})
}

// TestALiveDynamicLinkNeedsNoWait has a link whose last call is newer than
// the timeout, and one from a call the harness did not play. Neither may
// hold up the shot.
func TestALiveDynamicLinkNeedsNoWait(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := dynamicDB(t)
		for _, id := range []uint{300000201, 300000102} {
			require.NoError(t, db.Exec(fmt.Sprintf("INSERT INTO repeaters (id, ts1_dynamic_talkgroup_id) VALUES (%d, 91)", id)).Error)
		}
		start := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
		r := newRunner(options{}, nil, nil, "http://hub", start)
		r.heardGroup(300000201, false)
		r.cursor = start.Add(dynamicTimeout - time.Minute)
		began := time.Now()
		require.NoError(t, r.waitDynamic(t.Context(), db))
		assert.Equal(t, began, time.Now())
	})
}
