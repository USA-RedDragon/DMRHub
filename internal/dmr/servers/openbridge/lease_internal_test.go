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
package openbridge

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEgressLeaseMovesAfterACrash lets the holder of the egress lease stop
// renewing it without giving it up, as a replica that dies does. The other
// replica must take it within EgressLeaseTTL plus one renewal.
func TestEgressLeaseMovesAfterACrash(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	a := &Server{kv: store, id: "a"}
	b := &Server{kv: store, id: "b"}

	ctx := context.Background()
	require.True(t, a.holdLease(ctx))
	require.False(t, b.holdLease(ctx), "one replica holds the lease")
	require.True(t, a.holdLease(ctx), "the holder renews it")

	crash := time.Now()
	for !b.holdLease(ctx) {
		require.Less(t, time.Since(crash), 2*EgressLeaseTTL, "the lease never moved")
		time.Sleep(egressLeaseRenew)
	}
	gap := time.Since(crash)
	t.Logf("the lease moved after %s", gap.Round(time.Millisecond))
	assert.LessOrEqual(t, gap, EgressLeaseTTL+egressLeaseRenew+200*time.Millisecond)
	assert.False(t, a.holdLease(ctx), "the old holder does not get it back")
}
