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
)

func TestRoutePacketParrot(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)

	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)

	srvHandle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	defer h.UnregisterServer(models.RepeaterTypeMMDVM)

	h.ActivateRepeater(context.Background(), 100001)

	// Allow subscription goroutines to fully start
	time.Sleep(100 * time.Millisecond)

	// Send voice header to parrot
	headerPkt := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         1000001,
		Dst:         dmrconst.ParrotUser,
		Repeater:    100001,
		GroupCall:   true,
		Slot:        false,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead),
		StreamID:    77777,
		BER:         -1,
		RSSI:        -1,
	}
	h.RoutePacket(context.Background(), headerPkt, models.RepeaterTypeMMDVM)

	// Send some voice packets
	for i := uint(0); i < 3; i++ {
		vPkt := makeVoicePacket(dmrconst.ParrotUser, 77777, true, false)
		vPkt.DTypeOrVSeq = i
		h.RoutePacket(context.Background(), vPkt, models.RepeaterTypeMMDVM)
	}

	// Send voice terminator to trigger playback
	termPkt := makeVoiceTermPacket(dmrconst.ParrotUser, 77777, true, false)
	h.RoutePacket(context.Background(), termPkt, models.RepeaterTypeMMDVM)

	// Parrot has a 3-second delay; wait up to 6 seconds for the first packet
	select {
	case rp := <-srvHandle.Packets:
		assert.Equal(t, uint(100001), rp.RepeaterID)
	case <-time.After(6 * time.Second):
		t.Fatal("Timed out waiting for parrot playback")
	}
}
