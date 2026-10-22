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
	require.NoError(t, database.Model(&models.Repeater{}).Where("id = ?", 100004).Update("trusted", true).Error)

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
	require.NoError(t, database.Create(&models.Call{UserID: 1000002, RepeaterID: 100004, CreatedAt: time.Now()}).Error)

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
