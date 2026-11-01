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
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

// drainInSet is a KV store that runs during, once, before it stores the
// first value equal to at.
type drainInSet struct {
	kv.KV
	at     string
	during func()
}

func (d *drainInSet) Set(ctx context.Context, key string, value []byte) error {
	if string(value) == d.at && d.during != nil {
		during := d.during
		d.during = nil
		during()
	}
	return d.KV.Set(ctx, key, value) //nolint:wrapcheck // a test wrapper
}

// TestADrainDuringAHeartbeatStays marks a replica as draining while its
// heartbeat, which read that the replica serves, stores its key. The key
// must still say that the replica drains afterwards, or the other replicas
// never take its sessions over.
func TestADrainDuringAHeartbeatStays(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	const id = "instance-heartbeat"
	wrap := &drainInSet{KV: store, at: id}
	r := NewInstanceRegistry(ctx, wrap, id)
	t.Cleanup(func() { r.Deregister(ctx) })
	wrap.during = func() { r.MarkDraining(ctx) }
	require.NoError(t, r.write(ctx))
	require.True(t, InstanceDraining(ctx, store, id), "the heartbeat stored the replica as serving after it began to drain")
}
