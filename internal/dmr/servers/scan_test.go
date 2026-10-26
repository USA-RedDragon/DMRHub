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

package servers_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crowdedKV returns a KV store of the backend with many other keys in it,
// as the one of a hub that serves many repeaters.
func crowdedKV(t *testing.T, be testutils.Backend) kv.KV {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	be.Setup(t, &cfg)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	for i := range 500 {
		require.NoError(t, store.Set(context.Background(), fmt.Sprintf("other:%s:%d", t.Name(), i), []byte{1}))
	}
	return store
}

// TestOtherInstancesExistAmongManyKeys registers two instances in a KV
// store that holds many other keys. Each must see the other, however the
// store pages its scan, or a replica that stops takes itself for the last
// one and makes every repeater log in again.
func TestOtherInstancesExistAmongManyKeys(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			store := crowdedKV(t, be)
			ctx := context.Background()
			one := servers.NewInstanceRegistry(ctx, store, "scan-one-"+be.Name)
			defer one.Deregister(ctx)
			two := servers.NewInstanceRegistry(ctx, store, "scan-two-"+be.Name)
			defer two.Deregister(ctx)
			assert.True(t, one.OtherInstancesExist(ctx), "the first instance does not see the second")
			assert.True(t, two.OtherInstancesExist(ctx), "the second instance does not see the first")
		})
	}
}

// TestListRepeatersAmongManyKeys stores repeaters in a KV store that holds
// many other keys. ListRepeaters must return all of them.
func TestListRepeatersAmongManyKeys(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			store := crowdedKV(t, be)
			c := servers.MakeKVClient(store)
			ctx := context.Background()
			want := make([]uint, 0, 30)
			for i := range uint(30) {
				id := 900000 + i
				c.StoreRepeater(ctx, id, models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}})
				want = append(want, id)
			}
			got, err := c.ListRepeaters(ctx)
			require.NoError(t, err)
			for _, id := range want {
				assert.True(t, slices.Contains(got, id), "repeater %d is missing", id)
			}
		})
	}
}
