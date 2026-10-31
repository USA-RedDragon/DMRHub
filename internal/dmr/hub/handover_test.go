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

func TestALatePingAtTheOldReplicaKeepsTheNewOwner(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, database, store := replicas(t, 2)
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		ctx := context.Background()
		r[0].hub.ActivateRepeater(ctx, 100001)
		r[0].reg.MarkDraining(ctx)
		require.True(t, r[1].hub.TakeOver(ctx, 100001))
		r[1].hub.ActivateRepeater(ctx, 100001)

		r[0].hub.RenewOwnership(ctx, 100001)
		owner, err := store.Get(ctx, "dmrhub:peer_owner:100001")
		require.NoError(t, err)
		assert.Equal(t, "replica-2", string(owner), "a ping that the old replica handled after the takeover took the repeater back")
		assert.False(t, r[0].hub.OwnsRepeater(ctx, 100001))
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

// TestAMoveClaimOlderThanOurOwnHandsOver drains a replica that claimed a
// repeater, and has another replica take the session over and claim it for
// a time before that claim, as a browser hotspot that resumes with a drain
// notice of the same second as its login does: the notice time is whole
// seconds. The claim is not stale, since the move record names the
// replica that took the session over, and the draining replica must hand
// the repeater over.
func TestAMoveClaimOlderThanOurOwnHandsOver(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, r1, two, database := twoReplicas(t)
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		ctx := context.Background()
		gone := make(chan uint, 1)
		one.OnClaimedElsewhere(func(id uint, _ time.Time) { gone <- id })
		at := time.Now()
		one.ActivateRepeaterSince(ctx, 100001, at)
		r1.MarkDraining(ctx)
		require.True(t, two.TakeOver(ctx, 100001), "the other replica takes the session over")
		two.ActivateRepeaterSince(ctx, 100001, at.Add(-500*time.Millisecond))
		select {
		case id := <-gone:
			assert.Equal(t, uint(100001), id)
		case <-time.After(failAfter):
			t.Fatal("the draining replica did not hand over the repeater that moved")
		}
	})
}

// TestAClaimAtTheSameTimeAsOurOwnEndsOneSession has two replicas each
// claim a repeater, and then get the claim of the other replica for it with
// the same time as their own, as two replicas that stamp in the same tick
// do. Exactly one replica must keep the repeater: the one whose ID sorts
// last.
func TestAClaimAtTheSameTimeAsOurOwnEndsOneSession(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, database, store := replicas(t, 2)
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		seedRepeater(t, database, 100002, 1000001)
		ctx := context.Background()
		one, two := r[0].hub, r[1].hub
		toldOne := make(chan uint, 2)
		one.OnClaimedElsewhere(func(id uint, _ time.Time) { toldOne <- id })
		toldTwo := make(chan uint, 2)
		two.OnClaimedElsewhere(func(id uint, _ time.Time) { toldTwo <- id })
		at := time.Now()
		one.ActivateRepeaterSince(ctx, 100001, at)
		two.ActivateRepeaterSince(ctx, 100002, at)
		synctest.Wait()
		require.NoError(t, store.Set(ctx, "dmrhub:peer_owner:100001", []byte("replica-2")))
		require.NoError(t, store.Set(ctx, "dmrhub:peer_owner:100002", []byte("replica-1")))
		require.NoError(t, r[0].ps.Publish("hub:claimed", fmt.Appendf(nil, "100001 replica-2 %d", at.UnixNano())))
		require.NoError(t, r[0].ps.Publish("hub:claimed", fmt.Appendf(nil, "100002 replica-1 %d", at.UnixNano())))
		synctest.Wait()
		select {
		case id := <-toldOne:
			assert.Equal(t, uint(100001), id)
		default:
			t.Error("the replica whose ID sorts first kept a repeater that the other claimed at the same time")
		}
		select {
		case id := <-toldTwo:
			t.Errorf("the replica whose ID sorts last lost repeater %d to a claim at the same time as its own", id)
		default:
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

// handOverWaitFor starts a HandOverWait of 1 s with a limit of 10 s on two
// for repeater 100001, which one owns, runs after, and returns once the
// wait ended. Its setup takes no time in a bubble.
func handOverWaitFor(t *testing.T, draining bool, after func(r1 *servers.InstanceRegistry, store kv.KV)) {
	t.Helper()
	r, database, store := replicas(t, 2)
	one, r1, two := r[0].hub, r[0].reg, r[1].hub
	seedUser(t, database, 1000001, "N0CALL")
	seedRepeater(t, database, 100001, 1000001)
	ctx := context.Background()
	one.ActivateRepeater(ctx, 100001)
	if draining {
		r1.MarkDraining(ctx)
	}
	expired, stop := two.HandOverWait(ctx, 100001, time.Second, 10*time.Second)
	defer stop()
	after(r1, store)
	<-expired
}

func TestHandOverWaitLastsWhileTheOwnerDrains(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		handOverWaitFor(t, true, func(*servers.InstanceRegistry, kv.KV) {})
		took := time.Since(start)
		assert.Equal(t, 10*time.Second, took, "the wait ends at its limit while the owner drains")
	})
}

func TestHandOverWaitEndsSoonAfterTheOwnerStops(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		handOverWaitFor(t, true, func(r1 *servers.InstanceRegistry, _ kv.KV) {
			time.Sleep(3 * time.Second)
			r1.Deregister(context.Background())
		})
		took := time.Since(start)
		assert.InDelta(t, 4*time.Second, took, float64(100*time.Millisecond), "the wait ends 1 s after the owner deregistered")
	})
}

func TestHandOverWaitEndsSoonAfterTheOwnerDied(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		handOverWaitFor(t, true, func(r1 *servers.InstanceRegistry, store kv.KV) {
			time.Sleep(3 * time.Second)
			require.NoError(t, store.Delete(context.Background(), "dmrhub:instance:"+r1.ID()))
		})
		took := time.Since(start)
		assert.InDelta(t, 4*time.Second, took, float64(100*time.Millisecond), "the wait ends 1 s after the record of the owner expired")
	})
}

func TestHandOverWaitOfAnOwnerThatServes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		handOverWaitFor(t, false, func(*servers.InstanceRegistry, kv.KV) {})
		took := time.Since(start)
		assert.Equal(t, time.Second, took, "the wait for an owner that does not drain is the bare wait")
	})
}

// TestBarrierWaitsForASlowServer has a server take nothing for 3 s after a
// Barrier, as a starved replica does. The Barrier must return only once
// the server took the marker, so that a handover after it holds every
// packet that the server got for the repeater.
func TestBarrierWaitsForASlowServer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := hub.NewHub(nil, nil, nil, nil)
		t.Cleanup(func() { h.Stop(context.Background()) })
		srv := h.RegisterServer(hub.ServerConfig{Name: "mmdvm", Role: hub.RoleRepeater})
		start := time.Now()
		returned := make(chan time.Duration, 1)
		go func() {
			h.Barrier("mmdvm")
			returned <- time.Since(start)
		}()
		time.Sleep(3 * time.Second)
		rp := <-srv.Packets
		require.NotNil(t, rp.Barrier)
		close(rp.Barrier)
		assert.Equal(t, 3*time.Second, <-returned, "the Barrier returned before the server took it")
	})
}

// TestBarrierEndsWhenTheServerGoes unregisters a server that never takes a
// Barrier. The Barrier must return at once.
func TestBarrierEndsWhenTheServerGoes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := hub.NewHub(nil, nil, nil, nil)
		t.Cleanup(func() { h.Stop(context.Background()) })
		h.RegisterServer(hub.ServerConfig{Name: "mmdvm", Role: hub.RoleRepeater})
		start := time.Now()
		returned := make(chan time.Duration, 1)
		go func() {
			h.Barrier("mmdvm")
			returned <- time.Since(start)
		}()
		time.Sleep(300 * time.Millisecond)
		h.UnregisterServer("mmdvm")
		assert.Equal(t, 300*time.Millisecond, <-returned, "the Barrier outlived its server")
	})
}

// claimDropper is a PubSub that loses the claims of a replica.
type claimDropper struct{ pubsub.PubSub }

func (d claimDropper) Publish(topic string, message []byte) error {
	if topic == "hub:claimed" {
		return nil
	}
	return d.PubSub.Publish(topic, message) //nolint:wrapcheck
}

// TestAMissedClaimHandsOverFromTheClaimTime has a second replica claim a
// repeater that the first one serves while the claim notice is lost, and
// the first one wait for its repeaters to move as a draining replica does.
// The first replica must find the claim in the KV store and hand the
// repeater over from the time of the claim, not from when it found it, so
// that the handover holds what it sent since the move.
func TestAMissedClaimHandsOverFromTheClaimTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, database, store := replicas(t, 1)
		one, r1 := r[0].hub, r[0].reg
		two := hub.NewHub(database, store, claimDropper{r[0].ps}, nil)
		require.NoError(t, two.SetInstanceID("replica-2"))
		t.Cleanup(func() { two.Stop(context.Background()) })
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		ctx := context.Background()
		handed := make(chan time.Time, 1)
		one.OnClaimedElsewhere(func(_ uint, since time.Time) { handed <- since })
		one.ActivateRepeater(ctx, 100001)
		r1.MarkDraining(ctx)

		time.Sleep(300 * time.Millisecond)
		claimed := time.Now()
		two.ActivateRepeaterSince(ctx, 100001, claimed)
		go one.WaitMoved(ctx)
		select {
		case since := <-handed:
			assert.True(t, since.Equal(claimed), "handed over from %s, want the claim time %s", since, claimed)
		case <-time.After(failAfter):
			t.Fatal("the first replica did not hand the repeater over")
		}
	})
}

// TestAMissedClaimOfAnOlderReplicaHandsOver has a replica of an older
// DMRHub claim a repeater: it writes its plain ID as the owner and no
// claim time, and its claim notice is lost. The replica that served the
// repeater must still hand it over, from when it found the claim.
func TestAMissedClaimOfAnOlderReplicaHandsOver(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, database, store := replicas(t, 1)
		one, r1 := r[0].hub, r[0].reg
		seedUser(t, database, 1000001, "N0CALL")
		seedRepeater(t, database, 100001, 1000001)
		ctx := context.Background()
		handed := make(chan time.Time, 1)
		one.OnClaimedElsewhere(func(_ uint, since time.Time) { handed <- since })
		one.ActivateRepeater(ctx, 100001)
		r1.MarkDraining(ctx)

		claimed := time.Now()
		require.NoError(t, store.Set(ctx, "dmrhub:peer_owner:100001", []byte("replica-old")))
		go one.WaitMoved(ctx)
		select {
		case since := <-handed:
			assert.False(t, since.Before(claimed), "handed over from %s, before the claim at %s", since, claimed)
		case <-time.After(failAfter):
			t.Fatal("the first replica did not hand the repeater over")
		}
	})
}
