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
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func linkReplicas(t *testing.T, be testutils.Backend, n int) ([]*hub.Hub, *gorm.DB) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	be.Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		_ = store.Close()
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	hubs := make([]*hub.Hub, n)
	for i := range hubs {
		ct := calltracker.NewCallTracker(database, ps, store)
		h := hub.NewHub(database, store, ps, ct)
		h.LiftSlotRate(true)
		require.NoError(t, h.SetInstanceID(fmt.Sprintf("replica-%d", i+1)))
		h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		t.Cleanup(func() {
			h.UnregisterServer(models.RepeaterTypeMMDVM)
			h.Stop(context.Background())
			ct.Stop()
		})
		hubs[i] = h
	}
	seedUser(t, database, testSrcID, "TESTUSER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedTalkgroup(t, database, 10, "TG10")
	seedTalkgroup(t, database, 11, "TG11")
	return hubs, database
}

func dynamicTS2(t *testing.T, database *gorm.DB) uint {
	t.Helper()
	rpt, err := models.FindRepeaterByID(database, testRepeaterID)
	require.NoError(t, err)
	if rpt.TS2DynamicTalkgroupID == nil {
		return 0
	}
	return *rpt.TS2DynamicTalkgroupID
}

func lateBurst(t *testing.T, h *hub.Hub, database *gorm.DB, before, change func()) uint {
	t.Helper()
	first := makeVoicePacket(11, 0x9301, true, true)
	h.RoutePacket(context.Background(), first, models.RepeaterTypeMMDVM)
	settle(t, h, first, models.RepeaterTypeMMDVM)
	require.Equal(t, uint(11), dynamicTS2(t, database), "a call on TG 11 links it on TS2")
	before()

	late := makeVoicePacket(11, 0x9301, true, true)
	release := h.HoldSource(late, models.RepeaterTypeMMDVM)
	h.RoutePacketAt(context.Background(), late, models.RepeaterTypeMMDVM, time.Now().Add(-time.Millisecond))
	change()
	h.ForgetCachedRepeater(testRepeaterID)
	release()
	settle(t, h, late, models.RepeaterTypeMMDVM)
	return dynamicTS2(t, database)
}

func forLinkBackends(t *testing.T, f func(t *testing.T, be testutils.Backend)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			f(t, be)
		})
	}
}

// TestALateBurstDoesNotUndoALaterDynamicLinkChange checks that a burst of
// a call that the hub still holds when a later change of the dynamic
// talkgroup of the slot comes leaves the change: the burst came first.
func TestALateBurstDoesNotUndoALaterDynamicLinkChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(h *hub.Hub)
		want   uint
	}{
		{"unlink", func(h *hub.Hub) { h.UnlinkDynamicTalkgroup(context.Background(), testRepeaterID, true) }, 0},
		{"link", func(h *hub.Hub) { h.LinkDynamicTalkgroup(context.Background(), testRepeaterID, 10, true) }, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forLinkBackends(t, func(t *testing.T, be testutils.Backend) {
				hubs, database := linkReplicas(t, be, 1)
				h := hubs[0]
				h.ActivateRepeater(context.Background(), testRepeaterID)
				got := lateBurst(t, h, database, func() {}, func() { tc.change(h) })
				assert.Equal(t, tc.want, got, "a burst that came before the change undid it")
			})
		})
	}
}

// TestALateBurstOnTheOldReplicaDoesNotUndoAChangeOnTheNewOne makes the
// change on another replica that the repeater logged in to, after the
// replica that served the repeater before handed it over, while that one
// still held a burst of it.
func TestALateBurstOnTheOldReplicaDoesNotUndoAChangeOnTheNewOne(t *testing.T) {
	t.Parallel()
	forLinkBackends(t, func(t *testing.T, be testutils.Backend) {
		hubs, database := linkReplicas(t, be, 2)
		old, moved := hubs[0], hubs[1]
		old.ActivateRepeater(context.Background(), testRepeaterID)
		got := lateBurst(t, old, database, func() {
			moved.ActivateRepeater(context.Background(), testRepeaterID)
			require.Zero(t, old.WaitMoved(t.Context()), "the old replica keeps the repeater")
		}, func() {
			moved.UnlinkDynamicTalkgroup(context.Background(), testRepeaterID, true)
		})
		assert.Equal(t, uint(0), got, "a burst on the old replica undid the change of the new one")
	})
}

// TestASweepLeavesALinkThatANewerSessionMadeAfterItsRead runs the
// timeout sweep of a replica with no session of the repeater on TG 11 of
// TS2, and lets a newer session on another replica link TS2 between the
// read and the write of the sweep: to TG 10, and again to TG 11. The
// sweep must leave both.
func TestASweepLeavesALinkThatANewerSessionMadeAfterItsRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		tg   uint
	}{{"other", 10}, {"same", 11}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forLinkBackends(t, func(t *testing.T, be testutils.Backend) {
				hubs, database := linkReplicas(t, be, 2)
				sweeper, session := hubs[0], hubs[1]
				session.LinkDynamicTalkgroup(context.Background(), testRepeaterID, 11, true)
				require.Equal(t, uint(11), dynamicTS2(t, database))
				sweeper.ExpireDynamicAfter(testRepeaterID, true, 11, func() {
					session.ActivateRepeater(context.Background(), testRepeaterID)
					session.UnlinkDynamicTalkgroup(context.Background(), testRepeaterID, true)
					session.LinkDynamicTalkgroup(context.Background(), testRepeaterID, tc.tg, true)
				})
				assert.Equal(t, tc.tg, dynamicTS2(t, database), "the sweep undid the link of the newer session")
			})
		})
	}
}
