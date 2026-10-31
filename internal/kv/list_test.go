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

package kv_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forEachStore runs f on the in-memory store and on Redis.
func forEachStore(t *testing.T, f func(t *testing.T, store kv.KV)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			be.Setup(t, &cfg)
			store, err := kv.MakeKV(t.Context(), &cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			f(t, store)
		})
	}
}

func TestKVRPushTrimKeepsTheNewest(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := context.Background()
		for i := range 5 {
			require.NoError(t, store.RPushTrim(ctx, "trim", []byte{byte(i)}, 3, time.Minute))
		}
		got, err := store.LRange(ctx, "trim")
		require.NoError(t, err)
		assert.Equal(t, [][]byte{{2}, {3}, {4}}, got)
	})
}

func TestKVLRangeLeavesTheList(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := context.Background()
		require.NoError(t, store.RPushTrim(ctx, "keep", []byte("a"), 8, time.Minute))
		require.NoError(t, store.RPushTrim(ctx, "keep", []byte("b"), 8, time.Minute))
		for range 2 {
			got, err := store.LRange(ctx, "keep")
			require.NoError(t, err)
			assert.Equal(t, [][]byte{[]byte("a"), []byte("b")}, got)
		}
	})
}

func TestKVLRangeOfNoListIsEmpty(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		got, err := store.LRange(context.Background(), "none")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestKVRPushTrimSetsTheTTL(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := context.Background()
		require.NoError(t, store.RPushTrim(ctx, "ttl", []byte("a"), 8, time.Minute))
		expiry, err := kv.ExpiryOf(ctx, store, "ttl")
		require.NoError(t, err)
		assert.True(t, expiry.After(time.Now()), "the list has no TTL")
		assert.False(t, expiry.After(time.Now().Add(time.Minute+time.Second)), "the TTL is longer than asked")
		require.NoError(t, store.RPushTrim(ctx, "ttl", []byte("b"), 8, time.Hour))
		expiry, err = kv.ExpiryOf(ctx, store, "ttl")
		require.NoError(t, err)
		assert.True(t, expiry.After(time.Now().Add(time.Minute)), "the push did not refresh the TTL of the list")
	})
}

// TestKVRPushTrimIsOneStep pushes from many goroutines at once. Each push
// must land and the list must never hold more than keep entries.
func TestKVRPushTrimIsOneStep(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := context.Background()
		const writers, each, keep = 8, 50, 4
		var wg sync.WaitGroup
		for w := range writers {
			wg.Go(func() {
				for i := range each {
					assert.NoError(t, store.RPushTrim(ctx, "race", fmt.Appendf(nil, "%d-%d", w, i), keep, time.Minute))
					got, err := store.LRange(ctx, "race")
					assert.NoError(t, err)
					assert.LessOrEqual(t, len(got), keep)
				}
			})
		}
		wg.Wait()
		got, err := store.LRange(ctx, "race")
		require.NoError(t, err)
		assert.Len(t, got, keep)
	})
}

func TestKVLRangeOfAnExpiredListIsEmptyInMemory(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		store, err := kv.MakeKV(t.Context(), &cfg)
		require.NoError(t, err)
		defer func() { _ = store.Close() }()
		require.NoError(t, store.RPushTrim(t.Context(), "old", []byte("a"), 8, time.Second))
		time.Sleep(2 * time.Second)
		got, err := store.LRange(t.Context(), "old")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestKVDeleteIfEqualDeletesItsOwnValue(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := context.Background()
		require.NoError(t, store.Set(ctx, "lease", []byte("mine")))
		deleted, err := store.DeleteIfEqual(ctx, "lease", []byte("mine"))
		require.NoError(t, err)
		assert.True(t, deleted)
		_, err = store.Get(ctx, "lease")
		assert.ErrorIs(t, err, kv.ErrNotFound)
	})
}

func TestKVDeleteIfEqualKeepsAValueThatAnotherWriterSet(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := context.Background()
		require.NoError(t, store.Set(ctx, "lease", []byte("mine")))
		require.NoError(t, store.Set(ctx, "lease", []byte("theirs")))
		deleted, err := store.DeleteIfEqual(ctx, "lease", []byte("mine"))
		require.NoError(t, err)
		assert.False(t, deleted)
		got, err := store.Get(ctx, "lease")
		require.NoError(t, err)
		assert.Equal(t, []byte("theirs"), got)
	})
}

func TestKVDeleteIfEqualOfNoKey(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		deleted, err := store.DeleteIfEqual(context.Background(), "none", []byte("mine"))
		require.NoError(t, err)
		assert.False(t, deleted)
	})
}

func TestKVDeleteIfEqualKeepsASetNXValueOfAnotherWriter(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := context.Background()
		ok, err := store.SetNX(ctx, "lease", "theirs", time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		deleted, err := store.DeleteIfEqual(ctx, "lease", []byte("mine"))
		require.NoError(t, err)
		assert.False(t, deleted)
		deleted, err = store.DeleteIfEqual(ctx, "lease", []byte("theirs"))
		require.NoError(t, err)
		assert.True(t, deleted)
		ok, err = store.SetNX(ctx, "lease", "mine", time.Minute)
		require.NoError(t, err)
		assert.True(t, ok, "the key stayed after its owner deleted it")
	})
}

// TestKVAnExpiredKeyRemovalKeepsANewValue reads a key that expired while
// another goroutine sets it again and reads it back. Removing the expired
// value must never remove the new one.
func TestKVAnExpiredKeyRemovalKeepsANewValue(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()
	const rounds = 2000
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = store.Get(ctx, "flip")
				_, _ = store.Has(ctx, "flip")
				_, _, _ = store.Scan(ctx, 0, "flip", 10)
			}
		}
	})
	lost := 0
	for range rounds {
		require.NoError(t, store.Set(ctx, "flip", []byte("old")))
		require.NoError(t, store.Expire(ctx, "flip", time.Nanosecond))
		require.NoError(t, store.Set(ctx, "flip", []byte("new")))
		if _, err := store.Get(ctx, "flip"); err != nil {
			lost++
		}
	}
	close(stop)
	wg.Wait()
	assert.Zero(t, lost, "a removal of an expired value removed a value set after it")
}
