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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func dataHeader(src, dst, repeater, stream uint) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         src,
		Dst:         dst,
		Repeater:    repeater,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: 6,
		StreamID:    stream,
		BER:         -1,
		RSSI:        -1,
	}
}

func lastSeenHub(t *testing.T) (*hub.Hub, *gorm.DB, *hub.ServerHandle) {
	t.Helper()
	h, database := makeTestHub(t)
	seedUser(t, database, 1000001, "OTHER")
	seedUser(t, database, 1000002, "TARGET")
	seedRepeater(t, database, 100003, 1000002)
	seedRepeater(t, database, 100004, 1000001)
	srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	h.ActivateRepeater(context.Background(), 100003)
	h.ActivateRepeater(context.Background(), 100004)
	time.Sleep(100 * time.Millisecond)
	return h, database, srv
}

func expectAt(t *testing.T, srv *hub.ServerHandle, repeater uint, what string) {
	t.Helper()
	select {
	case rp := <-srv.Packets:
		assert.Equal(t, repeater, rp.RepeaterID, what)
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: nothing delivered", what)
	}
	select {
	case rp := <-srv.Packets:
		t.Fatalf("%s: extra packet to %d", what, rp.RepeaterID)
	case <-time.After(200 * time.Millisecond):
	}
}

func expectNone(t *testing.T, srv *hub.ServerHandle, what string) {
	t.Helper()
	select {
	case rp := <-srv.Packets:
		t.Fatalf("%s: delivered to %d", what, rp.RepeaterID)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestPrivateTrafficFindsRadioHeardOnlyThroughData(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)

	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100003, 1), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 2), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "private data to a radio heard on its owner's repeater")
}

func TestUntrustedSightingDoesNotMoveRadio(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)

	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100003, 1), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)
	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100004, 2), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 3), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "a sighting on another user's hotspot must not move the radio")
}

func TestTrustedRepeaterMovesRadio(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100004).Error)

	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100003, 1), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)
	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100004, 2), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100003, 3), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100004, "a sighting on a trusted repeater moves the radio")
}

func TestUntrustedCallRowDoesNotLocateRadio(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	userID, repeaterID := uint(1000002), uint(100004)
	require.NoError(t, database.Create(&models.Call{UserID: &userID, RepeaterID: &repeaterID, IngressID: repeaterID, CreatedAt: time.Now()}).Error)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100003, 1), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "a call on another user's untrusted hotspot must not locate the radio")
}

func TestPrivateTrafficToUnregisteredRadioIsDropped(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)

	h.RoutePacket(context.Background(), dataHeader(1234567, 1, 100003, 1), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)
	h.RoutePacket(context.Background(), dataHeader(1000002, 1234567, 100003, 2), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "private traffic to a radio that is not a user")
}

func TestUntrustingRepeaterStopsRouting(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100004).Error)

	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100004, 1), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", false, 100004).Error)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100003, 2), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "private traffic must not follow a sighting on a repeater that is no longer trusted")
}

func TestSightingIsNotHiddenByAnotherSourceOnTheSameStream(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1, 100004, 7), models.RepeaterTypeMMDVM)
	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100003, 7), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 8), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "a stream ID that another radio used first must not hide a sighting")
}

func TestPrivateStreamKeepsItsTargetWhenTheRadioMoves(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100004).Error)

	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100003, 1), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)
	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 2), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "the first burst goes where the radio was heard")

	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100004, 3), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)
	block := dataHeader(1000001, 1000002, 100004, 2)
	block.DTypeOrVSeq = 7
	h.RoutePacket(context.Background(), block, models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "the rest of the stream goes to the same repeater")

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 4), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100004, "a new stream goes where the radio is now")
}

func countTableQueries(t *testing.T, database *gorm.DB, table string) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:count_"+table, func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			n.Add(1)
		}
	}))
	return &n
}

func TestPrivateStreamLooksUpItsTargetOnce(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)

	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100003, 1), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)

	burst := dataHeader(1000001, 1000002, 100004, 2)
	h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "the first burst of the stream")

	users := countTableQueries(t, database, "users")
	for seq := uint(1); seq < 10; seq++ {
		burst.Seq = seq
		h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
		expectAt(t, srv, 100003, "a later burst of the stream")
	}
	assert.Zero(t, users.Load(), "user lookups after the first burst of a private stream")
}

func TestGroupStreamChecksItsTalkgroupOnce(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 1, "TG1")
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))
	srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	burst := makeVoicePacket(1, 55555, true, false)
	h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100002, "the first burst of the stream")

	talkgroups := countTableQueries(t, database, "talkgroups")
	for seq := uint(1); seq < 10; seq++ {
		burst.Seq = seq
		h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
		expectAt(t, srv, 100002, "a later burst of the stream")
	}
	assert.Zero(t, talkgroups.Load(), "talkgroup checks after the first burst of a group stream")
}
