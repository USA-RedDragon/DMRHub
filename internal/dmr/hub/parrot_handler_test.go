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

func parrotHub(t *testing.T) (*hub.Hub, *hub.ServerHandle) {
	t.Helper()
	h, database := makeTestHub(t)
	seedUser(t, database, testSrcID, "TESTUSER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	h.ActivateRepeater(context.Background(), testRepeaterID)
	time.Sleep(100 * time.Millisecond)
	return h, srv
}

func drainParrot(srv *hub.ServerHandle, wait time.Duration) []models.Packet {
	var got []models.Packet
	deadline := time.After(wait)
	for {
		select {
		case rp := <-srv.Packets:
			got = append(got, rp.Packet)
		case <-deadline:
			return got
		}
	}
}

func TestParrotIgnoresARepeatedTerminator(t *testing.T) {
	t.Parallel()
	h, srv := parrotHub(t)

	header := makeVoiceTermPacket(dmrconst.ParrotUser, 77001, false, false)
	header.DTypeOrVSeq = uint(dmrconst.DTypeVoiceHead)
	h.RoutePacket(context.Background(), header, models.RepeaterTypeMMDVM)
	for i := uint(0); i < 3; i++ {
		v := makeVoicePacket(dmrconst.ParrotUser, 77001, false, false)
		v.DTypeOrVSeq = i
		h.RoutePacket(context.Background(), v, models.RepeaterTypeMMDVM)
	}
	for range 2 {
		h.RoutePacket(context.Background(), makeVoiceTermPacket(dmrconst.ParrotUser, 77001, false, false), models.RepeaterTypeMMDVM)
	}

	got := drainParrot(srv, 5*time.Second)
	var terms int
	for _, p := range got {
		if p.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(p.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
			terms++
		}
	}
	assert.Equal(t, 1, terms, "one reply, which ends with its one terminator")
	if assert.NotEmpty(t, got) {
		assert.Equal(t, uint(dmrconst.DTypeVoiceHead), got[0].DTypeOrVSeq, "the reply starts with the header")
	}
}

func TestParrotDoesNotRecordFromATerminator(t *testing.T) {
	t.Parallel()
	h, srv := parrotHub(t)

	h.RoutePacket(context.Background(), makeVoiceTermPacket(dmrconst.ParrotUser, 77002, false, true), models.RepeaterTypeMMDVM)
	assert.Empty(t, drainParrot(srv, 4*time.Second), "a lone terminator is not a call to play back")
}

func TestParrotIgnoresNetworkPeers(t *testing.T) {
	t.Parallel()
	h, srv := parrotHub(t)
	peer := h.RegisterServer(hub.ServerConfig{Name: "peer-srv", Role: hub.RolePeer})
	t.Cleanup(func() { h.UnregisterServer("peer-srv") })

	header := makeVoiceTermPacket(dmrconst.ParrotUser, 77003, false, false)
	header.DTypeOrVSeq = uint(dmrconst.DTypeVoiceHead)
	header.Repeater = testRepeaterID
	h.RoutePacket(context.Background(), header, "peer-srv")
	term := makeVoiceTermPacket(dmrconst.ParrotUser, 77003, false, false)
	term.Repeater = testRepeaterID
	h.RoutePacket(context.Background(), term, "peer-srv")

	assert.Empty(t, drainParrot(srv, 4*time.Second), "a call to the parrot from a network peer is not played back")
	select {
	case rp := <-peer.Packets:
		t.Fatalf("the peer got %v", rp.Packet)
	default:
	}
}
