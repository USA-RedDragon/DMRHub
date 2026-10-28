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
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// slowTalkgroupQueries makes each query of the talkgroups table, once
// armed, take longer than a burst waits for a lookup.
func slowTalkgroupQueries(t *testing.T, database *gorm.DB) *atomic.Bool {
	t.Helper()
	var armed atomic.Bool
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:slow_talkgroups", func(tx *gorm.DB) {
		if armed.Load() && tx.Statement.Table == "talkgroups" {
			time.Sleep(lookup.Wait + 100*time.Millisecond)
		}
	}))
	return &armed
}

func seedListener(t *testing.T, database *gorm.DB, tg uint) {
	t.Helper()
	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, tg, "TG")
	rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002}}
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Replace([]models.Talkgroup{{ID: tg}}))
}

// startHub starts a hub on database and ps, as a restart of the hub does.
func startHub(t *testing.T, database *gorm.DB, ps pubsub.PubSub) *hub.Hub {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	h := hub.NewHub(database, kvStore, ps, ct)
	t.Cleanup(func() {
		h.Stop(context.Background())
		ct.Stop()
		_ = kvStore.Close()
	})
	h.LiftSlotRate(true)
	return h
}

func TestATalkgroupThatWasThereAtStartTakesItsFirstCallFromASlowDatabase(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, database, ps := makeTestHubWithPubSub(t)
		slow := slowTalkgroupQueries(t, database)
		seedListener(t, database, 7)

		h := startHub(t, database, ps)
		srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		defer h.UnregisterServer(models.RepeaterTypeMMDVM)
		h.ActivateRepeater(context.Background(), 100002)

		slow.Store(true)
		h.RoutePacket(context.Background(), makeVoicePacket(7, 62001, true, false), models.RepeaterTypeMMDVM)
		rp := receive(t, srv)
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(62001), rp.Packet.StreamID)
	})
}

func TestANewTalkgroupTakesItsFirstCallFromASlowDatabase(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		slow := slowTalkgroupQueries(t, database)
		seedListener(t, database, 8)
		h.TalkgroupChanged(8)

		srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		defer h.UnregisterServer(models.RepeaterTypeMMDVM)
		h.ActivateRepeater(context.Background(), 100002)

		slow.Store(true)
		h.RoutePacket(context.Background(), makeVoicePacket(8, 62002, true, false), models.RepeaterTypeMMDVM)
		rp := receive(t, srv)
		assert.Equal(t, uint(100002), rp.RepeaterID)
		assert.Equal(t, uint(62002), rp.Packet.StreamID)
	})
}
