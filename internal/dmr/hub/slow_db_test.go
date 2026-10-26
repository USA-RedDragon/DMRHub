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
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	slowStatement = 300 * time.Millisecond
	burstPeriod   = 60 * time.Millisecond
)

type slowDB struct{ on atomic.Bool }

func slowDown(t *testing.T, database *gorm.DB) *slowDB {
	t.Helper()
	return slowDownBy(t, database, slowStatement)
}

func slowDownBy(t *testing.T, database *gorm.DB, d time.Duration) *slowDB {
	t.Helper()
	s := &slowDB{}
	wait := func(*gorm.DB) {
		if s.on.Load() {
			time.Sleep(d)
		}
	}
	cb := database.Callback()
	require.NoError(t, cb.Query().Before("gorm:query").Register("test:slow_query", wait))
	require.NoError(t, cb.Row().Before("gorm:row").Register("test:slow_row", wait))
	require.NoError(t, cb.Raw().Before("gorm:raw").Register("test:slow_raw", wait))
	require.NoError(t, cb.Create().Before("gorm:create").Register("test:slow_create", wait))
	require.NoError(t, cb.Update().Before("gorm:update").Register("test:slow_update", wait))
	require.NoError(t, cb.Delete().Before("gorm:delete").Register("test:slow_delete", wait))
	return s
}

type arrival struct {
	server string
	rp     hub.RoutedPacket
	at     time.Time
}

func collect(handles map[string]*hub.ServerHandle) <-chan arrival {
	out := make(chan arrival, 1000)
	for name, handle := range handles {
		go func() {
			for rp := range handle.Packets {
				out <- arrival{server: name, rp: rp, at: time.Now()}
			}
		}()
	}
	return out
}

func sendStream(h *hub.Hub, dst, stream uint, group bool, n int) []time.Time {
	sent := make([]time.Time, n)
	for i := range n {
		pkt := makeVoicePacket(dst, stream, group, false)
		pkt.Seq = uint(i)
		sent[i] = time.Now()
		h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
		time.Sleep(burstPeriod)
	}
	return sent
}

func worstDelay(t *testing.T, arrivals <-chan arrival, stream uint, sent []time.Time, targets ...string) time.Duration {
	t.Helper()
	got := map[string]int{}
	var worst time.Duration
	deadline := time.After(time.Duration(len(sent))*burstPeriod + 30*time.Second)
	for {
		done := true
		for _, target := range targets {
			done = done && got[target] == len(sent)
		}
		if done {
			return worst
		}
		select {
		case a := <-arrivals:
			if a.rp.Packet.StreamID != stream {
				continue
			}
			target := fmt.Sprintf("%s:%d", a.server, a.rp.RepeaterID)
			if !slices.Contains(targets, target) {
				continue
			}
			got[target]++
			if d := a.at.Sub(sent[a.rp.Packet.Seq]); d > worst {
				worst = d
			}
		case <-deadline:
			t.Fatalf("stream %d: deliveries %v, want %d on each of %v", stream, got, len(sent), targets)
		}
	}
}

// TestSlowDatabaseHoldsNoBurst starts calls of each kind while every
// database statement takes 300 ms, after the hub has routed calls of the
// same kinds once. Routing must not wait on the database: each burst
// reaches its targets in less than half a statement.
func TestSlowDatabaseHoldsNoBurst(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	slow := slowDown(t, database)

	seedUser(t, database, testSrcID, "CALLER")
	seedUser(t, database, 1000002, "TARGET")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedRepeater(t, database, 100002, 1000002)
	seedTalkgroup(t, database, 1, "TG1")
	var rpt2 models.Repeater
	require.NoError(t, database.First(&rpt2, 100002).Error)
	require.NoError(t, database.Model(&rpt2).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))
	require.NoError(t, database.Create(&models.Peer{ID: 3100100, OwnerID: testSrcID, Egress: true}).Error)
	require.NoError(t, database.Create(&models.PeerRule{PeerID: 3100100, SubjectIDMin: 1, SubjectIDMax: 9999999}).Error)

	arrivals := collect(map[string]*hub.ServerHandle{
		"mmdvm": h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater}),
		"peers": h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer}),
	})
	h.ActivateRepeater(context.Background(), testRepeaterID)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	const bursts = 8
	calls := []struct {
		name    string
		dst     uint
		group   bool
		targets []string
	}{
		{"group", 1, true, []string{"mmdvm:100002", "peers:3100100"}},
		{"private to a repeater", 100002, false, []string{"mmdvm:100002", "peers:3100100"}},
		{"private to a radio", 1000002, false, []string{"mmdvm:100002", "peers:3100100"}},
	}
	userCall := models.Call{UserID: new(uint(1000002)), RepeaterID: new(uint(100002)), IngressID: 100002, CreatedAt: time.Now()}
	require.NoError(t, database.Create(&userCall).Error)

	stream := uint(70000)
	for _, c := range calls {
		stream++
		worstDelay(t, arrivals, stream, sendStream(h, c.dst, stream, c.group, bursts), c.targets...)
	}
	time.Sleep(200 * time.Millisecond)

	slow.on.Store(true)
	defer slow.on.Store(false)
	for _, c := range calls {
		stream++
		worst := worstDelay(t, arrivals, stream, sendStream(h, c.dst, stream, c.group, bursts), c.targets...)
		t.Logf("%s: worst delay %v with %v per statement", c.name, worst, slowStatement)
		assert.Less(t, worst, slowStatement/2, "%s: a burst waited on the database", c.name)
	}
}

func TestAStalledDatabaseHoldsNoBurstOfANewCall(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	slow := slowDownBy(t, database, 3*time.Second)

	seedUser(t, database, testSrcID, "CALLER")
	seedUser(t, database, 1000002, "TARGET")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedRepeater(t, database, 100002, 1000002)
	require.NoError(t, database.Create(&models.Call{UserID: new(uint(1000002)), RepeaterID: new(uint(100002)), IngressID: 100002, CreatedAt: time.Now()}).Error)
	arrivals := collect(map[string]*hub.ServerHandle{
		"mmdvm": h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater}),
	})
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	slow.on.Store(true)
	const stream = 70100
	sent := sendStream(h, 1000002, stream, false, 8)
	var worst time.Duration
	unstall := time.After(4 * time.Second)
	end := time.After(15 * time.Second)
	for waiting := true; waiting; {
		select {
		case a := <-arrivals:
			if a.rp.Packet.StreamID == stream {
				worst = max(worst, a.at.Sub(sent[a.rp.Packet.Seq]))
			}
		case <-unstall:
			slow.on.Store(false)
		case <-end:
			waiting = false
		}
	}
	assert.Less(t, worst, lookup.Wait+slowStatement, "a burst of a call to a radio waited on a stalled database")

	worstDelay(t, arrivals, stream+1, sendStream(h, 1000002, stream+1, false, 8), "mmdvm:100002")
}
