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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
)

func makeTestKV(t *testing.T) kv.KV {
	t.Helper()
	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	assert.NoError(t, err)

	t.Cleanup(func() {
		_ = kvStore.Close()
	})
	return kvStore
}

func TestKVSetAndGet(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()

	err := store.Set(ctx, "testkey", []byte("testvalue"))
	assert.NoError(t, err)

	val, err := store.Get(ctx, "testkey")
	assert.NoError(t, err)
	assert.Equal(t, "testvalue", string(val))
}

func TestKVGetNonexistent(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	_, err := store.Get(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestKVHas(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()

	has, err := store.Has(ctx, "missing")
	assert.NoError(t, err)
	assert.False(t, has)

	_ = store.Set(ctx, "present", []byte("val"))

	has, err = store.Has(ctx, "present")
	assert.NoError(t, err)
	assert.True(t, has)
}

func TestKVDelete(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()

	_ = store.Set(ctx, "delme", []byte("val"))

	err := store.Delete(ctx, "delme")
	assert.NoError(t, err)

	has, err := store.Has(ctx, "delme")
	assert.NoError(t, err)
	assert.False(t, has)
}

func TestKVExpire(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()

	_ = store.Set(ctx, "expiring", []byte("val"))

	err := store.Expire(ctx, "expiring", 50*time.Millisecond)
	assert.NoError(t, err)

	// Key should exist immediately
	has, _ := store.Has(ctx, "expiring")
	assert.True(t, has)

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	has, _ = store.Has(ctx, "expiring")
	assert.False(t, has)

	_, err = store.Get(ctx, "expiring")
	assert.Error(t, err)
}

func TestKVExpireNonexistent(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	err := store.Expire(context.Background(), "nope", time.Second)
	assert.Error(t, err)
}

func TestKVExpireZeroDeletesKey(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()

	_ = store.Set(ctx, "zerottl", []byte("val"))

	err := store.Expire(ctx, "zerottl", 0)
	assert.NoError(t, err)

	has, _ := store.Has(ctx, "zerottl")
	assert.False(t, has)
}

func TestKVScan(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()

	_ = store.Set(ctx, "scan:a", []byte("1"))
	_ = store.Set(ctx, "scan:b", []byte("2"))
	_ = store.Set(ctx, "other", []byte("3"))

	keys, _, err := store.Scan(ctx, 0, "scan:*", 100)
	assert.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestKVScanEmptyPattern(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()

	_ = store.Set(ctx, "a", []byte("1"))
	_ = store.Set(ctx, "b", []byte("2"))

	keys, _, err := store.Scan(ctx, 0, "", 100)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(keys), 2)
}

func TestKVOverwrite(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key", []byte("first"))
	_ = store.Set(ctx, "key", []byte("second"))

	val, err := store.Get(ctx, "key")
	assert.NoError(t, err)
	assert.Equal(t, "second", string(val))
}

func TestKVClose(t *testing.T) {
	t.Parallel()
	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	store, err := kv.MakeKV(context.Background(), &defConfig)
	assert.NoError(t, err)

	err = store.Close()
	assert.NoError(t, err)
}

// --- Benchmarks ---

func makeTestKVB(b *testing.B) kv.KV {
	b.Helper()
	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		b.Fatalf("Failed to create default config: %v", err)
	}
	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	if err != nil {
		b.Fatalf("Failed to create kv: %v", err)
	}
	b.Cleanup(func() {
		_ = kvStore.Close()
	})
	return kvStore
}

func BenchmarkKVSet(b *testing.B) {
	store := makeTestKVB(b)
	val := []byte("benchmark-value-data")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Set(ctx, "bench-key", val)
	}
}

func BenchmarkKVGet(b *testing.B) {
	store := makeTestKVB(b)
	ctx := context.Background()
	_ = store.Set(ctx, "bench-key", []byte("benchmark-value-data"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Get(ctx, "bench-key")
	}
}

func BenchmarkKVHas(b *testing.B) {
	store := makeTestKVB(b)
	ctx := context.Background()
	_ = store.Set(ctx, "bench-key", []byte("benchmark-value-data"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Has(ctx, "bench-key")
	}
}

// Regression tests: KV interface now accepts context.Context, ensuring
// callers can propagate cancellation and deadlines into KV operations.

func TestKVContextPassedToAllMethods(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	// Use a non-Background context to confirm the interface accepts it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// All methods should work with a derived context.
	err := store.Set(ctx, "ctx-test", []byte("value"))
	assert.NoError(t, err)

	val, err := store.Get(ctx, "ctx-test")
	assert.NoError(t, err)
	assert.Equal(t, "value", string(val))

	has, err := store.Has(ctx, "ctx-test")
	assert.NoError(t, err)
	assert.True(t, has)

	err = store.Expire(ctx, "ctx-test", 10*time.Second)
	assert.NoError(t, err)

	keys, _, err := store.Scan(ctx, 0, "ctx-test*", 100)
	assert.NoError(t, err)
	assert.Contains(t, keys, "ctx-test")

	err = store.Delete(ctx, "ctx-test")
	assert.NoError(t, err)

	has, err = store.Has(ctx, "ctx-test")
	assert.NoError(t, err)
	assert.False(t, has)
}

func TestKVCancelledContextReturnsCleanly(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	// Pre-populate a key so we can test reads against a cancelled context.
	err := store.Set(context.Background(), "cancel-test", []byte("data"))
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// For the in-memory backend the context is currently unused, so operations
	// still succeed. This test documents the contract: passing a cancelled
	// context must not panic. If a Redis backend is used in the future, these
	// would return context.Canceled errors instead.
	_, _ = store.Get(ctx, "cancel-test")
	_, _ = store.Has(ctx, "cancel-test")
	_ = store.Set(ctx, "cancel-test2", []byte("x"))
	_ = store.Delete(ctx, "cancel-test")
	_ = store.Expire(ctx, "cancel-test2", time.Second)
	keys, _, scanErr := store.Scan(ctx, 0, "*", 10)
	_ = keys
	_ = scanErr
}
