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
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type replica struct {
	hub *hub.Hub
	reg *servers.InstanceRegistry
	ps  pubsub.PubSub
}

// replicas returns n hubs that share a database, a KV store and pubsub,
// each registered as a replica.
func replicas(t *testing.T, n int) ([]replica, *gorm.DB, kv.KV) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
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
		require.NoError(t, h.SetInstanceID(id))
		r := servers.NewInstanceRegistry(context.Background(), store, id)
		t.Cleanup(func() {
			r.Deregister(context.Background())
			h.Stop(context.Background())
		})
		out[i] = replica{hub: h, reg: r, ps: ps}
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
	synctest.Test(t, func(t *testing.T) {
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
		case <-time.After(failAfter):
			t.Fatal("WaitMoved did not return after both repeaters moved")
		}
	})
}

func TestWaitMovedEndsWithItsContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, _, _, database := twoReplicas(t)
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		one.ActivateRepeater(context.Background(), 100001)

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		assert.Equal(t, 1, one.WaitMoved(ctx), "a repeater that never moved is still here")
	})
}

func TestWaitMovedReturnsWhenARepeaterLeaves(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
		case <-time.After(failAfter):
			t.Fatal("WaitMoved did not return after the repeater left")
		}
	})
}

func TestTakeOverGoesToOneReplica(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
	})
}

func TestRenewOwnershipWritesTheClaimAgain(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
	})
}

func TestOnClaimedElsewhereEndsTheOldSession(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
		case <-time.After(failAfter):
			t.Fatal("the old owner was not told")
		}
		select {
		case id := <-mine:
			t.Fatalf("a replica was told of its own claim of %d", id)
		case id := <-gone:
			t.Fatalf("the old owner was told of %d too", id)
		case <-time.After(200 * time.Millisecond):
		}
	})
}

// TestAClaimOlderThanOurOwnEndsNothing claims a repeater, and then gets a
// claim of another replica for a packet that came before the one that made
// this replica claim it, while the KV store still names the other replica,
// as when that claim comes late and this replica has not yet written its
// own. The claim is stale and must not end the new session here.
func TestAClaimOlderThanOurOwnEndsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, database, store := replicas(t, 2)
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		ctx := context.Background()
		two := r[1].hub
		told := make(chan uint, 1)
		two.OnClaimedElsewhere(func(id uint, _ time.Time) { told <- id })
		at := time.Now()
		two.ActivateRepeaterSince(ctx, 100001, at)
		require.NoError(t, store.Set(ctx, "dmrhub:peer_owner:100001", []byte("replica-1")))
		msg := fmt.Sprintf("100001 replica-1 %d", at.Add(-time.Millisecond).UnixNano())
		require.NoError(t, r[1].ps.Publish("hub:claimed", []byte(msg)))
		select {
		case id := <-told:
			t.Fatalf("a claim older than this replica's own ended its session of %d", id)
		case <-time.After(300 * time.Millisecond):
		}
	})
}

// TestWaitMovedWaitsForTheHandOverOfAClaimItMissed claims a repeater in the
// KV store only, as when the claim on pubsub is late or lost. WaitMoved
// must not return before the servers handed the repeater over, or the
// replica stops and the packets that it sent after the move are lost.
func TestWaitMovedWaitsForTheHandOverOfAClaimItMissed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, database, store := replicas(t, 2)
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		ctx := context.Background()
		one := r[0].hub
		handed := make(chan uint, 1)
		one.OnClaimedElsewhere(func(id uint, _ time.Time) {
			time.Sleep(100 * time.Millisecond)
			handed <- id
		})
		one.ActivateRepeater(ctx, 100001)
		r[0].reg.MarkDraining(ctx)
		require.NoError(t, store.Set(ctx, "dmrhub:peer_owner:100001", []byte("replica-2")))

		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		assert.Zero(t, one.WaitMoved(wctx))
		select {
		case id := <-handed:
			assert.Equal(t, uint(100001), id)
		default:
			t.Fatal("WaitMoved returned before the repeater was handed over")
		}
	})
}

// TestWaitMovedWaitsForTheHandOverOfAClaim claims a repeater on another
// replica. WaitMoved must return only once the servers handed it over.
func TestWaitMovedWaitsForTheHandOverOfAClaim(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, r1, two, database := twoReplicas(t)
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		ctx := context.Background()
		handed := make(chan uint, 1)
		one.OnClaimedElsewhere(func(id uint, _ time.Time) {
			time.Sleep(100 * time.Millisecond)
			handed <- id
		})
		one.ActivateRepeater(ctx, 100001)
		r1.MarkDraining(ctx)

		left := make(chan int, 1)
		go func() { left <- one.WaitMoved(ctx) }()
		time.Sleep(50 * time.Millisecond)
		two.ActivateRepeater(ctx, 100001)
		select {
		case n := <-left:
			assert.Zero(t, n)
		case <-time.After(failAfter):
			t.Fatal("WaitMoved did not return after the repeater moved")
		}
		select {
		case <-handed:
		default:
			t.Fatal("WaitMoved returned before the repeater was handed over")
		}
	})
}

// TestSetInstanceIDFailsOnceAServerStarted sets the ID while a server
// goroutine waits on a socket, as a test harness that registered a
// replica after its servers started did. The socket gives no ordering, so
// the server must never see the write.
func TestSetInstanceIDFailsOnceAServerStarted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := hub.NewHub(nil, nil, nil, nil)
		t.Cleanup(func() { h.Stop(context.Background()) })
		require.NoError(t, h.SetInstanceID("replica-one"))
		h.RegisterServer(hub.ServerConfig{Name: "mmdvm", Role: hub.RoleRepeater})
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		read := make(chan bool)
		go func() {
			buf := make([]byte, 16)
			_, _, _ = conn.ReadFromUDP(buf)
			read <- h.OwnerLeaving(context.Background(), 1)
		}()
		require.ErrorIs(t, h.SetInstanceID("replica-two"), hub.ErrServersStarted)
		addr, ok := conn.LocalAddr().(*net.UDPAddr)
		require.True(t, ok)
		out, err := net.DialUDP("udp", nil, addr)
		require.NoError(t, err)
		t.Cleanup(func() { _ = out.Close() })
		_, err = out.Write([]byte("DMRD"))
		require.NoError(t, err)
		assert.False(t, <-read)
		assert.Equal(t, "replica-one", h.InstanceID())
	})
}
