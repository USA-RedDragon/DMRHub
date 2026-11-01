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
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	ingressWait  = time.Second
	ingressLimit = 10 * time.Second
)

type movingRig struct {
	old      *hub.Hub
	oldReg   *servers.InstanceRegistry
	now      *hub.Hub
	arrivals <-chan arrival
}

func trackedReplicas(t *testing.T) (*hub.Hub, *servers.InstanceRegistry, *hub.Hub, *gorm.DB) {
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
	hubs := make([]*hub.Hub, 2)
	regs := make([]*servers.InstanceRegistry, 2)
	for i := range hubs {
		id := fmt.Sprintf("replica-%d", i+1)
		ct := calltracker.NewCallTracker(database, ps, store)
		h := hub.NewHub(database, store, ps, ct)
		require.NoError(t, h.SetInstanceID(id))
		r := servers.NewInstanceRegistry(context.Background(), store, id)
		t.Cleanup(func() {
			r.Deregister(context.Background())
			h.Stop(context.Background())
			ct.Stop()
		})
		hubs[i], regs[i] = h, r
	}
	return hubs[0], regs[0], hubs[1], database
}

func startMovingRig(t *testing.T, draining bool) movingRig {
	t.Helper()
	old, oldReg, now, database := trackedReplicas(t)
	rig := movingRig{old: old, oldReg: oldReg, now: now}
	seedUser(t, database, testSrcID, "CALLER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedTalkgroup(t, database, 1, "TG1")
	require.NoError(t, database.Create(&models.Peer{ID: orderPeerID, OwnerID: testSrcID, Egress: true}).Error)
	require.NoError(t, database.Create(&models.PeerRule{PeerID: orderPeerID, SubjectIDMin: 1, SubjectIDMax: 9999999}).Error)
	rig.now.PeerChanged(orderPeerID)
	rig.arrivals = collect(t, map[string]*hub.ServerHandle{
		"peers": rig.now.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer}),
	})
	rig.now.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	ctx := context.Background()
	rig.old.ActivateRepeater(ctx, testRepeaterID)
	if draining {
		rig.oldReg.MarkDraining(ctx)
	}
	bubble.Eventually(t, func() bool { return rig.now.LookupsLoaded([]uint{1}, nil) }, 30*time.Second, 10*time.Millisecond, "the hub did not load the talkgroups and peers")
	return rig
}

func (r movingRig) move(t *testing.T, stream uint, n int) func() {
	t.Helper()
	ctx := context.Background()
	release := r.now.HoldIngress(ctx, models.RepeaterTypeMMDVM, testRepeaterID, ingressWait, ingressLimit)
	r.now.ActivateRepeater(ctx, testRepeaterID)
	for i := range n {
		p := makeVoicePacket(1, stream, true, false)
		p.Seq = uint(i)
		r.now.RoutePacket(ctx, p, models.RepeaterTypeMMDVM)
	}
	return release
}

func (r movingRig) none(t *testing.T) {
	t.Helper()
	synctest.Wait()
	select {
	case a := <-r.arrivals:
		t.Fatalf("burst %d of stream %d went out while the ingress was held", a.rp.Packet.Seq, a.rp.Packet.StreamID)
	default:
	}
}

func (r movingRig) receive(t *testing.T, stream uint, n int) ([]uint, time.Time) {
	t.Helper()
	var seqs []uint
	var at time.Time
	for len(seqs) < n {
		a := bubble.Receive(t, r.arrivals, 30*time.Second, "bursts %v, want %d", seqs, n)
		if a.rp.Packet.StreamID == stream {
			seqs = append(seqs, a.rp.Packet.Seq)
			at = a.at
		}
	}
	return seqs, at
}

// TestMovedIngressGoesOutAtTheHandover routes bursts of a repeater that
// moved here while the replica that served it drains. They must wait for
// the handover, then go out in order before the bursts that come after
// it.
func TestMovedIngressGoesOutAtTheHandover(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rig := startMovingRig(t, true)
		release := rig.move(t, 7401, 3)
		rig.none(t)
		release()
		p := makeVoicePacket(1, 7401, true, false)
		p.Seq = 3
		rig.now.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		seqs, _ := rig.receive(t, 7401, 4)
		assert.Equal(t, []uint{0, 1, 2, 3}, seqs, "the order of the bursts of the moved repeater")
	})
}

// TestMovedIngressWaitsForASlowOldReplica holds the handover of a draining
// replica, as a starved one is slow, for longer than the bare wait. The
// bursts must wait for it while the old replica drains.
func TestMovedIngressWaitsForASlowOldReplica(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rig := startMovingRig(t, true)
		release := rig.move(t, 7402, 2)
		time.Sleep(5 * ingressWait)
		rig.none(t)
		release()
		seqs, _ := rig.receive(t, 7402, 2)
		assert.Equal(t, []uint{0, 1}, seqs)
	})
}

// TestMovedIngressGoesOutWhenTheOldReplicaIsGone stops the draining
// replica that served the repeater before it sends a handover. The bursts
// must go out the bare wait after, not be lost.
func TestMovedIngressGoesOutWhenTheOldReplicaIsGone(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rig := startMovingRig(t, true)
		start := time.Now()
		rig.move(t, 7403, 2)
		time.Sleep(3 * time.Second)
		rig.none(t)
		rig.oldReg.Deregister(context.Background())
		seqs, at := rig.receive(t, 7403, 2)
		assert.Equal(t, []uint{0, 1}, seqs)
		assert.InDelta(t, 4*time.Second, at.Sub(start), float64(200*time.Millisecond), "the bursts go out the bare wait after the old replica went")
	})
}

// TestMovedIngressGoesOutWhenTheOldReplicaRefuses claims a repeater that
// another replica serves and does not give up: it claims the repeater
// again, so it sends no handover. The bursts must go out after the bare
// wait, not be lost.
func TestMovedIngressGoesOutWhenTheOldReplicaRefuses(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rig := startMovingRig(t, false)
		start := time.Now()
		rig.move(t, 7404, 2)
		rig.old.ActivateRepeater(context.Background(), testRepeaterID)
		seqs, at := rig.receive(t, 7404, 2)
		assert.Equal(t, []uint{0, 1}, seqs)
		assert.InDelta(t, ingressWait, at.Sub(start), float64(200*time.Millisecond), "the bursts go out the bare wait after the move")
	})
}

func releaseHub(t *testing.T) (*hub.Hub, *hub.ServerHandle) {
	t.Helper()
	h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV { return k })
	seedUser(t, database, testSrcID, "CALLER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedTalkgroup(t, database, 1, "TG1")
	require.NoError(t, database.Create(&models.Peer{ID: orderPeerID, OwnerID: testSrcID, Egress: true}).Error)
	require.NoError(t, database.Create(&models.PeerRule{PeerID: orderPeerID, SubjectIDMin: 1, SubjectIDMax: 9999999}).Error)
	h.PeerChanged(orderPeerID)
	peers := h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer})
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	h.ActivateRepeater(context.Background(), testRepeaterID)
	bubble.Eventually(t, func() bool { return h.LookupsLoaded([]uint{1}, nil) }, 30*time.Second, 10*time.Millisecond, "the hub did not load the talkgroups and peers")
	return h, peers
}

// TestReleaseIngressWaitsForThePeerServer routes a burst of a repeater
// whose peer server takes nothing for 3 s, as a starved one does.
// ReleaseIngress must return only once the server took the burst and then
// the marker after it.
func TestReleaseIngressWaitsForThePeerServer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, peers := releaseHub(t)
		h.RoutePacket(context.Background(), makeVoicePacket(1, 7405, true, false), models.RepeaterTypeMMDVM)
		start := time.Now()
		returned := make(chan bool, 1)
		go func() { returned <- h.ReleaseIngress(models.RepeaterTypeMMDVM, testRepeaterID, ingressLimit) }()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("ReleaseIngress returned before the peer server took the burst")
		default:
		}
		rp := <-peers.Packets
		require.Nil(t, rp.Barrier, "the marker came before the burst")
		assert.Equal(t, uint(7405), rp.Packet.StreamID)
		rp = <-peers.Packets
		require.NotNil(t, rp.Barrier, "no marker came after the burst")
		close(rp.Barrier)
		assert.True(t, <-returned, "ReleaseIngress gave up")
		assert.Equal(t, 3*time.Second, time.Since(start))
	})
}

// TestReleaseIngressGivesUpAtItsLimit routes a burst of a repeater whose
// peer server never takes it. ReleaseIngress must give up at its limit.
func TestReleaseIngressGivesUpAtItsLimit(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _ := releaseHub(t)
		h.RoutePacket(context.Background(), makeVoicePacket(1, 7406, true, false), models.RepeaterTypeMMDVM)
		start := time.Now()
		assert.False(t, h.ReleaseIngress(models.RepeaterTypeMMDVM, testRepeaterID, ingressLimit))
		assert.Equal(t, ingressLimit, time.Since(start))
	})
}
