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

package ratelimit_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/ratelimit"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func backends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

func newStore(t *testing.T, be testutils.Backend, limit uint) (*ratelimit.GORMStore, *gorm.DB) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	be.Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return ratelimit.NewGORMStore(&ratelimit.GORMOptions{DB: database, Rate: time.Minute, Limit: limit}), database
}

func TestLimitHoldsForConcurrentRequests(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			store, _ := newStore(t, be, 10)
			var allowed atomic.Int32
			var wg sync.WaitGroup
			for range 120 {
				wg.Go(func() {
					if !store.Limit("10.0.0.1", nil).RateLimited {
						allowed.Add(1)
					}
				})
			}
			wg.Wait()
			assert.Equal(t, int32(10), allowed.Load())
		})
	}
}

func TestLimitCountsAndResets(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			store, database := newStore(t, be, 2)
			first := store.Limit("10.0.0.2", nil)
			assert.False(t, first.RateLimited)
			assert.Equal(t, uint(1), first.RemainingHits)
			assert.False(t, store.Limit("10.0.0.2", nil).RateLimited)
			third := store.Limit("10.0.0.2", nil)
			assert.True(t, third.RateLimited)
			assert.Equal(t, uint(0), third.RemainingHits)
			assert.False(t, store.Limit("10.0.0.3", nil).RateLimited, "another address has its own count")

			require.NoError(t, database.Model(&models.Ratelimit{Key: "10.0.0.2"}).Update("timestamp", time.Now().Add(-2*time.Minute)).Error)
			assert.False(t, store.Limit("10.0.0.2", nil).RateLimited, "the count resets after the rate")
		})
	}
}

func TestSteadyHitsGetANewWindowEachRate(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			_, database := newStore(t, be, 3)
			store := ratelimit.NewGORMStore(&ratelimit.GORMOptions{DB: database, Rate: 300 * time.Millisecond, Limit: 3})
			for range 3 {
				require.False(t, store.Limit("10.0.0.2", nil).RateLimited)
				time.Sleep(120 * time.Millisecond)
			}
			assert.False(t, store.Limit("10.0.0.2", nil).RateLimited, "300 ms after the first hit a new window starts, however steady the hits")
		})
	}
}
