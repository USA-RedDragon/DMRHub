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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// slowPeerQueries makes each query of the peers table, once armed, take
// longer than a burst waits for a lookup.
func slowPeerQueries(t *testing.T, database *gorm.DB) *atomic.Bool {
	t.Helper()
	var armed atomic.Bool
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:slow_peers", func(tx *gorm.DB) {
		if armed.Load() && tx.Statement.Table == "peers" {
			time.Sleep(lookup.Wait + 100*time.Millisecond)
		}
	}))
	return &armed
}

func TestAPeerThatWasThereAtStartTakesTheFirstCallFromASlowDatabase(t *testing.T) {
	t.Parallel()
	_, database, ps := makeTestHubWithPubSub(t)
	slow := slowPeerQueries(t, database)
	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 1, "TG1")
	owner := models.User{ID: 2000001, Callsign: "PEEROWNER", Username: "PEEROWNER", Approved: true}
	require.NoError(t, database.Create(&owner).Error)
	peer := models.Peer{ID: 200, OwnerID: owner.ID, Egress: true}
	require.NoError(t, database.Create(&peer).Error)
	require.NoError(t, database.Create(&models.PeerRule{PeerID: peer.ID, Direction: false, SubjectIDMin: 1000000, SubjectIDMax: 2000000}).Error)

	h := startHub(t, database, ps)
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)
	peerSrv := h.RegisterServer(hub.ServerConfig{Name: "peer-srv", Role: hub.RolePeer})
	defer h.UnregisterServer("peer-srv")

	slow.Store(true)
	h.RoutePacket(context.Background(), makeVoicePacket(1, 62101, true, false), models.RepeaterTypeMMDVM)
	rp := receive(t, peerSrv)
	assert.Equal(t, peer.ID, rp.RepeaterID)
	assert.Equal(t, uint(62101), rp.Packet.StreamID)
}
