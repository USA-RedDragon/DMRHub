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

package hub_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type replica struct {
	hub *hub.Hub
	reg *servers.InstanceRegistry
}

// replicas returns n hubs that share a database, a KV store and pubsub,
// each registered as a replica.
func replicas(t *testing.T, n int) ([]replica, *gorm.DB, kv.KV) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = store.Close()
	})
	out := make([]replica, n)
	for i := range out {
		id := fmt.Sprintf("replica-%d", i+1)
		h := hub.NewHub(database, store, ps, nil)
		h.SetInstanceID(id)
		r := servers.NewInstanceRegistry(context.Background(), store, id)
		t.Cleanup(func() {
			r.Deregister(context.Background())
			h.Stop(context.Background())
		})
		out[i] = replica{hub: h, reg: r}
	}
	return out, database, store
}

// twoReplicas returns two hubs that share a database, a KV store and
// pubsub, each registered as a replica, and the registry of the first.
func twoReplicas(t *testing.T) (*hub.Hub, *servers.InstanceRegistry, *hub.Hub, *gorm.DB) {
	t.Helper()
	r, database, _ := replicas(t, 2)
	return r[0].hub, r[0].reg, r[1].hub, database
}

func TestWaitMovedReturnsWhenAnotherReplicaClaims(t *testing.T) {
	t.Parallel()
	one, r1, two, database := twoReplicas(t)
	seedUser(t, database, 1000001, "N0CALL")
	seedRepeater(t, database, 100001, 1000001)
	seedRepeater(t, database, 100002, 1000001)
	ctx := context.Background()
	one.ActivateRepeater(ctx, 100001)
	one.ActivateRepeater(ctx, 100002)
	r1.MarkDraining(ctx)

	assert.True(t, two.OwnerLeaving(ctx, 100001), "the owner drains")
	assert.False(t, one.OwnerLeaving(ctx, 100001), "a replica does not leave itself")

	left := make(chan int, 1)
	go func() { left <- one.WaitMoved(ctx) }()
	two.ActivateRepeater(ctx, 100001)
	select {
	case n := <-left:
		t.Fatalf("WaitMoved returned with %d repeaters before the second moved", n)
	case <-time.After(200 * time.Millisecond):
	}
	assert.False(t, one.OwnerLeaving(ctx, 100001), "the new owner serves")
	two.ActivateRepeater(ctx, 100002)
	select {
	case n := <-left:
		assert.Zero(t, n)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitMoved did not return after both repeaters moved")
	}
}

func TestWaitMovedEndsWithItsContext(t *testing.T) {
	t.Parallel()
	one, _, _, database := twoReplicas(t)
	seedUser(t, database, 1000001, "N0CALL")
	seedRepeater(t, database, 100001, 1000001)
	one.ActivateRepeater(context.Background(), 100001)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	assert.Equal(t, 1, one.WaitMoved(ctx), "a repeater that never moved is still here")
}

func TestWaitMovedReturnsWhenARepeaterLeaves(t *testing.T) {
	t.Parallel()
	one, _, _, database := twoReplicas(t)
	seedUser(t, database, 1000001, "N0CALL")
	seedRepeater(t, database, 100001, 1000001)
	ctx := context.Background()
	one.ActivateRepeater(ctx, 100001)

	left := make(chan int, 1)
	go func() { left <- one.WaitMoved(ctx) }()
	time.Sleep(100 * time.Millisecond)
	one.DeactivateRepeater(ctx, 100001)
	select {
	case n := <-left:
		assert.Zero(t, n)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitMoved did not return after the repeater left")
	}
}

func TestTakeOverGoesToOneReplica(t *testing.T) {
	t.Parallel()
	r, database, _ := replicas(t, 3)
	seedUser(t, database, 1000001, "N0CALL")
	seedRepeater(t, database, 100001, 1000001)
	ctx := context.Background()
	r[0].hub.ActivateRepeater(ctx, 100001)

	assert.False(t, r[1].hub.TakeOver(ctx, 100001), "an owner that serves keeps the repeater")
	r[0].reg.MarkDraining(ctx)
	assert.True(t, r[1].hub.TakeOver(ctx, 100001), "the first replica takes the repeater of a draining owner")
	assert.False(t, r[2].hub.TakeOver(ctx, 100001), "a second replica does not")
	assert.False(t, r[0].hub.TakeOver(ctx, 100001), "nor does the owner")

	r[0].reg.Deregister(ctx)
	r[1].hub.DeactivateRepeater(ctx, 100001)
	assert.False(t, r[2].hub.OwnerLeaving(ctx, 100001), "an owner that stopped hands nothing over")
}

func TestRenewOwnershipWritesTheClaimAgain(t *testing.T) {
	t.Parallel()
	r, database, store := replicas(t, 2)
	seedUser(t, database, 1000001, "N0CALL")
	seedRepeater(t, database, 100001, 1000001)
	ctx := context.Background()
	r[0].hub.ActivateRepeater(ctx, 100001)
	r[0].reg.MarkDraining(ctx)
	require.True(t, r[1].hub.OwnerLeaving(ctx, 100001))

	require.NoError(t, store.Delete(ctx, "dmrhub:peer_owner:100001"))
	require.False(t, r[1].hub.OwnerLeaving(ctx, 100001), "a claim that expired names no owner")
	r[0].hub.RenewOwnership(ctx, 100001)
	assert.True(t, r[1].hub.OwnerLeaving(ctx, 100001), "a renewed claim names its owner")
}

func TestOnClaimedElsewhereEndsTheOldSession(t *testing.T) {
	t.Parallel()
	one, _, two, database := twoReplicas(t)
	seedUser(t, database, 1000001, "N0CALL")
	seedRepeater(t, database, 100001, 1000001)
	seedRepeater(t, database, 100002, 1000001)
	ctx := context.Background()
	gone := make(chan uint, 4)
	one.OnClaimedElsewhere(func(id uint, _ time.Time) { gone <- id })
	mine := make(chan uint, 4)
	two.OnClaimedElsewhere(func(id uint, _ time.Time) { mine <- id })
	one.ActivateRepeater(ctx, 100001)

	two.ActivateRepeater(ctx, 100002)
	two.ActivateRepeater(ctx, 100001)
	select {
	case id := <-gone:
		assert.Equal(t, uint(100001), id, "only a repeater that is active here is reported")
	case <-time.After(2 * time.Second):
		t.Fatal("the old owner was not told")
	}
	select {
	case id := <-mine:
		t.Fatalf("a replica was told of its own claim of %d", id)
	case id := <-gone:
		t.Fatalf("the old owner was told of %d too", id)
	case <-time.After(200 * time.Millisecond):
	}
}
