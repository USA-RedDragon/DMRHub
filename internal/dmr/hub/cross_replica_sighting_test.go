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
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type heldSightings struct {
	kv.KV
	armed atomic.Bool
	open  chan struct{}
}

func (k *heldSightings) Set(ctx context.Context, key string, value []byte) error {
	if k.armed.Load() && strings.HasPrefix(key, "dmrhub:lastheard:") {
		<-k.open
	}
	return k.KV.Set(ctx, key, value) //nolint:wrapcheck
}

func TestAnAnswerOnAnotherReplicaFollowsASightingThatIsNotStoredYet(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		memdb.Use(t, &cfg)
		database, err := db.MakeDB(&cfg)
		require.NoError(t, err)
		ps, err := pubsub.MakePubSub(context.Background(), &cfg)
		require.NoError(t, err)
		store, err := kv.MakeKV(context.Background(), &cfg, kv.WithoutCallBound())
		require.NoError(t, err)
		held := &heldSightings{KV: store, open: make(chan struct{})}
		seedUser(t, database, 1000001, "SENDER")
		seedUser(t, database, 1000002, "ANSWER")
		seedRepeater(t, database, 100003, 1000001)
		seedRepeater(t, database, 100004, 1000002)
		require.NoError(t, database.Create(&models.UserRadioID{UserID: 1000001, RadioID: 1000101}).Error)
		ctOne, ctTwo := calltracker.NewCallTracker(database, ps, store), calltracker.NewCallTracker(database, ps, store)
		one, two := hub.NewHub(database, held, ps, ctOne), hub.NewHub(database, held, ps, ctTwo)
		one.LiftSlotRate(true)
		two.LiftSlotRate(true)
		require.NoError(t, one.SetInstanceID("one"))
		require.NoError(t, two.SetInstanceID("two"))
		srvOne := one.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		srvTwo := two.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		t.Cleanup(func() {
			held.armed.Store(false)
			close(held.open)
			one.Stop(context.Background())
			two.Stop(context.Background())
			ctOne.Stop()
			ctTwo.Stop()
			_ = ps.Close()
			_ = store.Close()
			if sqlDB, err := database.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		ctx := context.Background()
		one.ActivateRepeater(ctx, 100003)
		two.ActivateRepeater(ctx, 100004)
		for _, h := range []*hub.Hub{one, two} {
			require.NoError(t, h.KnowRepeater(100003))
			require.NoError(t, h.KnowRepeater(100004))
			loadLookups(t, h, nil, []uint{1000101, 1000002})
		}
		hear(t, two, 1000002, 100004, 1)

		held.armed.Store(true)
		one.RoutePacket(ctx, dataHeader(1000101, 1000002, 100003, 2), models.RepeaterTypeMMDVM)
		rp := bubble.Receive(t, srvTwo.Packets, deliveryWait, "the message did not reach the repeater of its target")
		assert.Equal(t, uint(100004), rp.RepeaterID)

		two.RoutePacket(ctx, dataHeader(1000002, 1000101, 100004, 3), models.RepeaterTypeMMDVM)
		rp = bubble.Receive(t, srvOne.Packets, deliveryWait, "the answer did not reach the repeater where its target was just heard")
		assert.Equal(t, uint(100003), rp.RepeaterID)
		assert.Equal(t, uint(3), rp.Packet.StreamID)
	})
}
