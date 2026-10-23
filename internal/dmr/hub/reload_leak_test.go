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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReloadedRepeaterGetsEachBurstOnce(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	gate := gateRepeaterLookups(t, database, 100002, 100002)
	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 1, "TG1")
	rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002}}
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Replace([]models.Talkgroup{{ID: 1}}))

	handle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)
	h.ActivateRepeater(context.Background(), 100002)
	h.ReloadRepeater(context.Background(), 100002)

	gate.armed.Store(true)
	held := makeVoicePacket(1, 60001, true, false)
	held.Repeater = 999998
	h.RoutePacket(context.Background(), held, models.RepeaterTypeMMDVM)
	gate.waitHeld(t, 1)
	gate.armed.Store(false)
	h.ReloadRepeater(context.Background(), 100002)
	gate.release()
	time.Sleep(100 * time.Millisecond)
	h.ReloadRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)
	for len(handle.Packets) > 0 {
		<-handle.Packets
	}

	pkt := makeVoicePacket(1, 60002, true, false)
	pkt.Repeater = 999998
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	got := 0
	deadline := time.After(500 * time.Millisecond)
	for done := false; !done; {
		select {
		case rp := <-handle.Packets:
			if rp.RepeaterID == 100002 && rp.Packet.Dst == 1 {
				got++
			}
		case <-deadline:
			done = true
		}
	}
	assert.Equal(t, 1, got, "a reload must not leave an old subscription delivering")
}
