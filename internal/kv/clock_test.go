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

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheInMemoryStoreExpiresKeysOnItsClock(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	store, err := kv.MakeKV(context.Background(), &config.Config{}, kv.WithClock(fake))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := t.Context()

	ok, err := store.SetNX(ctx, "lease", "a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, store.Set(ctx, "state", []byte("x")))
	require.NoError(t, store.Expire(ctx, "state", time.Minute))

	fake.Advance(time.Minute - time.Nanosecond)
	ok, err = store.SetNX(ctx, "lease", "b", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "the lease holds until its TTL")
	has, err := store.Has(ctx, "state")
	require.NoError(t, err)
	assert.True(t, has)

	fake.Advance(2 * time.Nanosecond)
	ok, err = store.SetNX(ctx, "lease", "b", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "an expired lease is free")
	has, err = store.Has(ctx, "state")
	require.NoError(t, err)
	assert.False(t, has, "a key expires at its TTL")
	_, err = store.Get(ctx, "state")
	require.ErrorIs(t, err, kv.ErrNotFound)
	keys, _, err := store.Scan(ctx, 0, "", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"lease"}, keys)
}
