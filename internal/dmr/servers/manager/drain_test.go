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

package manager_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRegistry struct {
	others   bool
	draining atomic.Bool
}

func (f *fakeRegistry) OtherInstancesExist(context.Context) bool { return f.others }
func (f *fakeRegistry) MarkDraining(context.Context)             { f.draining.Store(true) }

func TestDrainWaitsForTheRepeatersToMove(t *testing.T) {
	t.Parallel()
	l := testutils.SetupRegisteredLiveStack(t)
	l.SeedUser(t, 1000001, "N0CALL")
	l.SeedMMDVMRepeater(t, 100001, 1000001, password)
	c := testutils.NewMMDVMClient(100001, "", password)
	require.NoError(t, c.Connect(l.Addr(t, manager.MMDVM)))
	defer c.Close()
	require.NoError(t, c.WaitReady(wait))
	ctx := context.Background()
	require.Eventually(t, func() bool {
		v, err := l.KV.Get(ctx, "dmrhub:peer_owner:100001")
		return err == nil && len(v) > 0
	}, wait, 10*time.Millisecond, "the hub took the repeater on")

	lone := &fakeRegistry{}
	start := time.Now()
	assert.Zero(t, l.Manager.Drain(ctx, lone, time.Minute), "a lone instance does not drain")
	assert.False(t, lone.draining.Load())
	assert.Less(t, time.Since(start), time.Second)

	now := &fakeRegistry{others: true}
	assert.Zero(t, l.Manager.Drain(ctx, now, 0), "a timeout of 0 does not wait")
	assert.True(t, now.draining.Load(), "the replica is marked as draining all the same")

	waits := &fakeRegistry{others: true}
	start = time.Now()
	assert.Equal(t, 1, l.Manager.Drain(ctx, waits, 300*time.Millisecond), "the repeater did not move")
	assert.True(t, waits.draining.Load())
	assert.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond, "the drain waits for the repeater")
}
