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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func publishBurst(ps pubsub.PubSub, topic string, pkt models.Packet) error {
	raw := models.RawDMRPacket{Data: pkt.Encode()}
	b, err := raw.MarshalMsg(nil)
	if err != nil {
		return err //nolint:wrapcheck
	}
	return ps.Publish(topic, b) //nolint:wrapcheck
}

func queriesRepeater(tx *gorm.DB, id uint) bool {
	if tx.Statement.Table != "repeaters" {
		return false
	}
	for _, v := range tx.Statement.Vars {
		if v == id {
			return true
		}
	}
	return false
}

// TestAReloadLosesNoBurst publishes a private and a group burst to a
// repeater while ReloadRepeater reads it from the database, after the
// reload began and before it ended. The repeater must get each once.
func TestAReloadLosesNoBurst(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database, ps := makeTestHubWithPubSub(t)
		private := makeVoicePacket(100002, 61001, false, false)
		group := makeVoicePacket(1, 61002, true, false)
		var armed atomic.Bool
		var reads atomic.Int32
		published := make(chan error, 1)
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:publish_in_reload", func(tx *gorm.DB) {
			if !armed.Load() || !queriesRepeater(tx, 100002) || reads.Add(1) != 2 {
				return
			}
			err := publishBurst(ps, "hub:packets:repeater:100002", private)
			if err == nil {
				err = publishBurst(ps, "hub:packets:talkgroup:1", group)
			}
			published <- err
		}))

		seedUser(t, database, 1000001, "TESTUSER")
		seedRepeater(t, database, 100001, 1000001)
		seedRepeater(t, database, 100002, 1000001)
		seedTalkgroup(t, database, 1, "TG1")
		rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002}}
		require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Replace([]models.Talkgroup{{ID: 1}}))

		srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		defer h.UnregisterServer(models.RepeaterTypeMMDVM)
		h.ActivateRepeater(context.Background(), 100001)
		h.ActivateRepeater(context.Background(), 100002)
		knowTalkgroups(t, h)

		armed.Store(true)
		h.ReloadRepeater(context.Background(), 100002)
		armed.Store(false)
		select {
		case err := <-published:
			require.NoError(t, err)
		case <-time.After(failAfter):
			t.Fatal("the reload did not read the repeater twice")
		}

		got := map[uint]int{}
		for _, rp := range flush(t, h, srv, makeVoicePacket(1, 61003, true, false), 100002) {
			if rp.RepeaterID == 100002 {
				got[rp.Packet.StreamID]++
			}
		}
		assert.Equal(t, map[uint]int{private.StreamID: 1, group.StreamID: 1}, got, "the repeater gets each burst published during a reload once")
	})
}
