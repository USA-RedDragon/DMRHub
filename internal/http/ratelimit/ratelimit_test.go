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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/http/ratelimit"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newStore(t *testing.T, limit uint) (*ratelimit.GORMStore, *gorm.DB) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:ratelimit_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	return ratelimit.NewGORMStore(&ratelimit.GORMOptions{DB: database, Rate: time.Minute, Limit: limit}), database
}

func TestLimitHoldsForConcurrentRequests(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t, 10)
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
}

func TestLimitCountsAndResets(t *testing.T) {
	t.Parallel()
	store, database := newStore(t, 2)
	first := store.Limit("10.0.0.2", nil)
	assert.False(t, first.RateLimited)
	assert.Equal(t, uint(1), first.RemainingHits)
	assert.False(t, store.Limit("10.0.0.2", nil).RateLimited)
	third := store.Limit("10.0.0.2", nil)
	assert.True(t, third.RateLimited)
	assert.Equal(t, uint(0), third.RemainingHits)
	assert.False(t, store.Limit("10.0.0.3", nil).RateLimited, "another address has its own count")

	require.NoError(t, database.Exec("UPDATE ratelimits SET timestamp = ? WHERE key = ?", time.Now().Add(-2*time.Minute), "10.0.0.2").Error)
	assert.False(t, store.Limit("10.0.0.2", nil).RateLimited, "the count resets after the rate")
}
