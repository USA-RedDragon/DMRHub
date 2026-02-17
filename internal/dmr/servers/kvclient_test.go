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

package servers

import (
	"context"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestKV(t *testing.T) kv.KV {
	t.Helper()
	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = kvStore.Close()
	})
	return kvStore
}

func TestRepeaterKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "mmdvm:repeater:12345", repeaterKey(12345))
	assert.Equal(t, "mmdvm:repeater:0", repeaterKey(0))
	assert.Equal(t, "mmdvm:repeater:999999", repeaterKey(999999))
}

func TestKVClientStoreAndGetRepeater(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	client := MakeKVClient(store)
	ctx := context.Background()

	repeater := models.Repeater{
		Connection: models.RepeaterStateConnected,
	}
	repeater.ID = 12345

	client.StoreRepeater(ctx, 12345, repeater)

	got, err := client.GetRepeater(ctx, 12345)
	require.NoError(t, err)
	assert.Equal(t, uint(12345), got.ID)
	assert.Equal(t, models.RepeaterStateConnected, got.Connection)
}

func TestKVClientRepeaterExists(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	client := MakeKVClient(store)
	ctx := context.Background()

	assert.False(t, client.RepeaterExists(ctx, 99999))

	client.StoreRepeater(ctx, 99999, models.Repeater{})
	assert.True(t, client.RepeaterExists(ctx, 99999))
}

func TestKVClientDeleteRepeater(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	client := MakeKVClient(store)
	ctx := context.Background()

	client.StoreRepeater(ctx, 55555, models.Repeater{})
	assert.True(t, client.RepeaterExists(ctx, 55555))

	ok := client.DeleteRepeater(ctx, 55555)
	assert.True(t, ok)
	assert.False(t, client.RepeaterExists(ctx, 55555))
}

func TestKVClientDeleteNonexistentRepeater(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	client := MakeKVClient(store)
	ctx := context.Background()

	ok := client.DeleteRepeater(ctx, 11111)
	assert.True(t, ok)
}

func TestKVClientGetNonexistentRepeater(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	client := MakeKVClient(store)
	ctx := context.Background()

	_, err := client.GetRepeater(ctx, 77777)
	assert.ErrorIs(t, err, ErrNoSuchRepeater)
}

func TestKVClientListRepeaters(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	client := MakeKVClient(store)
	ctx := context.Background()

	client.StoreRepeater(ctx, 100, models.Repeater{})
	client.StoreRepeater(ctx, 200, models.Repeater{})

	repeaters, err := client.ListRepeaters(ctx)
	require.NoError(t, err)
	assert.Len(t, repeaters, 2)
	assert.ElementsMatch(t, []uint{100, 200}, repeaters)
}

func TestKVClientUpdateRepeaterConnection(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	client := MakeKVClient(store)
	ctx := context.Background()

	client.StoreRepeater(ctx, 300, models.Repeater{
		Connection: models.RepeaterStateLoginReceived,
	})

	client.UpdateRepeaterConnection(ctx, 300, models.RepeaterStateConnected)

	got, err := client.GetRepeater(ctx, 300)
	require.NoError(t, err)
	assert.Equal(t, models.RepeaterStateConnected, got.Connection)
}

// TestKeyFormatConsistency verifies that the key helpers produce keys
// that are correctly parsed back by ListRepeaters (regression test for
// the deduplication of the fmt.Sprintf("mmdvm:repeater:%d",...) pattern).
func TestKeyFormatConsistency(t *testing.T) {
	t.Parallel()
	store := makeTestKV(t)
	client := MakeKVClient(store)
	ctx := context.Background()

	ids := []uint{1, 42, 311860}
	for _, id := range ids {
		r := models.Repeater{}
		r.ID = id
		client.StoreRepeater(ctx, id, r)
	}

	listed, err := client.ListRepeaters(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, ids, listed)

	// Verify each can be individually retrieved
	for _, id := range ids {
		got, err := client.GetRepeater(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, id, got.ID)
	}
}
