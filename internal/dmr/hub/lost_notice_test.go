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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noticeDropper loses every repeater change notice, as Redis pubsub does
// while a replica is disconnected from it.
type noticeDropper struct{ pubsub.PubSub }

func (d noticeDropper) Publish(topic string, message []byte) error {
	if topic == "hub:repeaters:changed" {
		return nil
	}
	return d.PubSub.Publish(topic, message) //nolint:wrapcheck
}

func reaches(h *hub.Hub, handle *hub.ServerHandle, tg, stream uint) bool {
	pkt := makeVoicePacket(tg, stream, true, false)
	pkt.Repeater = 999998
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case rp := <-handle.Packets:
			if rp.RepeaterID == 100002 && rp.Packet.Dst == tg {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestLostNoticeHealsAfterTheCacheExpires(t *testing.T) {
	t.Parallel()
	hubB, database, ps := makeTestHubWithPubSub(t)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	kvA, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ctA := calltracker.NewCallTracker(database, ps, kvA)
	hubA := hub.NewHub(database, kvA, noticeDropper{ps}, ctA)
	t.Cleanup(func() {
		hubA.Stop(context.Background())
		ctA.Stop()
		_ = kvA.Close()
	})

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 1, "TG1")
	seedTalkgroup(t, database, 2, "TG2")
	rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002}}
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Replace([]models.Talkgroup{{ID: 1}}))

	handle := hubB.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer hubB.UnregisterServer(models.RepeaterTypeMMDVM)
	hubB.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)
	require.True(t, reaches(hubA, handle, 1, 50001))

	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Replace([]models.Talkgroup{{ID: 2}}))
	hubA.ReloadRepeater(context.Background(), 100002)
	time.Sleep(11 * time.Second)

	assert.False(t, reaches(hubA, handle, 1, 50002), "the removed talkgroup stops once the cache expires")
	assert.True(t, reaches(hubA, handle, 2, 50003), "the added talkgroup reaches the repeater once the cache expires")
}
