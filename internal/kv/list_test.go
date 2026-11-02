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
	"errors"
	"fmt"
	"strconv"
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

func TestKVRPushTrimRefusesAKeepBelowOne(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := context.Background()
		for _, keep := range []int64{0, -1} {
			assert.Error(t, store.RPushTrim(ctx, "bad", []byte("a"), keep, time.Minute), "keep %d", keep)
		}
		got, err := store.LRange(ctx, "bad")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestKVUpdateSetsTheValueAndTTL(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := bounded(t)
		var seen [][]byte
		for _, v := range []string{"a", "b"} {
			require.NoError(t, store.Update(ctx, "up", time.Minute, func(old []byte) ([]byte, error) {
				seen = append(seen, old)
				return append(append([]byte(nil), old...), v...), nil
			}))
		}
		assert.Equal(t, [][]byte{nil, []byte("a")}, seen)
		got, err := store.Get(ctx, "up")
		require.NoError(t, err)
		assert.Equal(t, []byte("ab"), got)
		expiry, err := kv.ExpiryOf(ctx, store, "up")
		require.NoError(t, err)
		assert.True(t, expiry.After(time.Now()), "the key has no TTL")
		assert.False(t, expiry.After(time.Now().Add(time.Minute+time.Second)), "the TTL is longer than asked")
	})
}

func TestKVUpdateWithAnErrorChangesNothing(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := bounded(t)
		require.NoError(t, store.Set(ctx, "up", []byte("a")))
		boom := errors.New("boom")
		err := store.Update(ctx, "up", time.Minute, func([]byte) ([]byte, error) { return []byte("b"), boom })
		require.ErrorIs(t, err, boom)
		got, err := store.Get(ctx, "up")
		require.NoError(t, err)
		assert.Equal(t, []byte("a"), got)
	})
}

// TestKVUpdateIsOneStep counts up one key from many goroutines at once.
// No update may be lost.
func TestKVUpdateIsOneStep(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := bounded(t)
		const writers, each = 8, 25
		var wg sync.WaitGroup
		for range writers {
			wg.Go(func() {
				for range each {
					err := store.Update(ctx, "count", time.Minute, func(old []byte) ([]byte, error) {
						n, _ := strconv.Atoi(string(old))
						return strconv.AppendInt(nil, int64(n+1), 10), nil
					})
					assert.NoError(t, err)
				}
			})
		}
		wg.Wait()
		got, err := store.Get(ctx, "count")
		require.NoError(t, err)
		assert.Equal(t, strconv.Itoa(writers*each), string(got))
	})
}

// TestKVUpdateOutlastsAKeyThatChangesOnManyTriesOnRedis changes the key
// from within fn on its first 24 runs, more than any fixed count of tries
// that a busy key can use up. Update must keep trying while its context
// lasts and store the result of the run that found the key unchanged.
func TestKVUpdateOutlastsAKeyThatChangesOnManyTriesOnRedis(t *testing.T) {
	t.Parallel()
	store := redisStore(t)
	ctx := bounded(t)
	const changes = 24
	runs := 0
	err := store.Update(ctx, "fight", time.Minute, func([]byte) ([]byte, error) {
		runs++
		if runs <= changes {
			require.NoError(t, store.Set(ctx, "fight", []byte("theirs")))
		}
		return []byte("mine"), nil
	})
	require.NoError(t, err)
	assert.Equal(t, changes+1, runs)
	got, err := store.Get(ctx, "fight")
	require.NoError(t, err)
	assert.Equal(t, []byte("mine"), got)
}

// TestKVUpdateGivesUpWhenItsContextEndsOnRedis changes the key from within
// fn on every run and ends the context of Update on the fifth run. Update
// must return ErrConflict with the error of the context and leave the
// other writer's value.
func TestKVUpdateGivesUpWhenItsContextEndsOnRedis(t *testing.T) {
	t.Parallel()
	store := redisStore(t)
	ctx, cancel := context.WithCancel(bounded(t))
	defer cancel()
	runs := 0
	err := store.Update(ctx, "fight", time.Minute, func([]byte) ([]byte, error) {
		runs++
		require.NoError(t, store.Set(context.Background(), "fight", []byte("theirs")))
		if runs == 5 {
			cancel()
		}
		return []byte("mine"), nil
	})
	require.ErrorIs(t, err, kv.ErrConflict)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 5, runs)
	got, err := store.Get(context.Background(), "fight")
	require.NoError(t, err)
	assert.Equal(t, []byte("theirs"), got)
}

// TestKVUpdateRefusesAContextWithoutADeadline checks that Update on each
// store refuses a ctx that could let it try forever, and changes nothing.
func TestKVUpdateRefusesAContextWithoutADeadline(t *testing.T) {
	t.Parallel()
	forEachStore(t, func(t *testing.T, store kv.KV) {
		ctx := t.Context()
		require.NoError(t, store.Set(ctx, "up", []byte("a")))
		runs := 0
		err := store.Update(ctx, "up", time.Minute, func([]byte) ([]byte, error) { runs++; return []byte("b"), nil })
		require.ErrorIs(t, err, kv.ErrNoDeadline)
		assert.Zero(t, runs)
		got, err := store.Get(ctx, "up")
		require.NoError(t, err)
		assert.Equal(t, []byte("a"), got)
	})
}

// bounded returns the context of t with the deadline of the test, the
// bound that Update needs.
func bounded(t *testing.T) context.Context {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().AddDate(1, 0, 0)
	}
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	t.Cleanup(cancel)
	return ctx
}

func redisStore(t *testing.T) kv.KV {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	testutils.PostgresRedisBackend().Setup(t, &cfg)
	store, err := kv.MakeKV(t.Context(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestKVUpdateOutlastsManyWritersOnRedis has 32 writers update one key on
// Redis at once. Every update must get through.
func TestKVUpdateOutlastsManyWritersOnRedis(t *testing.T) {
	t.Parallel()
	store := redisStore(t)
	const writers, each = 32, 25
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			for range each {
				err := store.Update(bounded(t), "many", time.Minute, func(old []byte) ([]byte, error) {
					n, _ := strconv.Atoi(string(old))
					return strconv.AppendInt(nil, int64(n+1), 10), nil
				})
				assert.NoError(t, err)
			}
		})
	}
	wg.Wait()
	got, err := store.Get(context.Background(), "many")
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(writers*each), string(got))
}
