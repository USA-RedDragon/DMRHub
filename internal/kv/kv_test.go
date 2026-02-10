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

	err := store.Set("testkey", []byte("testvalue"))
	assert.NoError(t, err)

	val, err := store.Get("testkey")
	assert.NoError(t, err)
	assert.Equal(t, "testvalue", string(val))
}

func TestKVGetNonexistent(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	_, err := store.Get("nonexistent")
	assert.Error(t, err)
}

func TestKVHas(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	has, err := store.Has("missing")
	assert.NoError(t, err)
	assert.False(t, has)

	_ = store.Set("present", []byte("val"))

	has, err = store.Has("present")
	assert.NoError(t, err)
	assert.True(t, has)
}

func TestKVDelete(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	_ = store.Set("delme", []byte("val"))

	err := store.Delete("delme")
	assert.NoError(t, err)

	has, err := store.Has("delme")
	assert.NoError(t, err)
	assert.False(t, has)
}

func TestKVExpire(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	_ = store.Set("expiring", []byte("val"))

	err := store.Expire("expiring", 50*time.Millisecond)
	assert.NoError(t, err)

	// Key should exist immediately
	has, _ := store.Has("expiring")
	assert.True(t, has)

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	has, _ = store.Has("expiring")
	assert.False(t, has)

	_, err = store.Get("expiring")
	assert.Error(t, err)
}

func TestKVExpireNonexistent(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	err := store.Expire("nope", time.Second)
	assert.Error(t, err)
}

func TestKVExpireZeroDeletesKey(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	_ = store.Set("zerottl", []byte("val"))

	err := store.Expire("zerottl", 0)
	assert.NoError(t, err)

	has, _ := store.Has("zerottl")
	assert.False(t, has)
}

func TestKVScan(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	_ = store.Set("scan:a", []byte("1"))
	_ = store.Set("scan:b", []byte("2"))
	_ = store.Set("other", []byte("3"))

	keys, _, err := store.Scan(0, "scan:*", 100)
	assert.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestKVScanEmptyPattern(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	_ = store.Set("a", []byte("1"))
	_ = store.Set("b", []byte("2"))

	keys, _, err := store.Scan(0, "", 100)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(keys), 2)
}

func TestKVOverwrite(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)

	_ = store.Set("key", []byte("first"))
	_ = store.Set("key", []byte("second"))

	val, err := store.Get("key")
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
