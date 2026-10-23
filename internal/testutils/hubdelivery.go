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

package testutils

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
)

// HubDelivery is an MMDVM server registered with a hub, so a test can see
// what the hub delivers to MMDVM repeaters.
type HubDelivery struct {
	hub    *hub.Hub
	handle *hub.ServerHandle
	stream uint
}

// NewHubDelivery registers an MMDVM server with h for the length of the test.
func NewHubDelivery(t *testing.T, h *hub.Hub) *HubDelivery {
	t.Helper()
	handle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	return &HubDelivery{hub: h, handle: handle, stream: 60000}
}

// GroupBurstReaches routes one group voice burst to tg from a repeater that
// does not exist and reports whether the hub delivered it to repeaterID.
func (d *HubDelivery) GroupBurstReaches(t *testing.T, tg, repeaterID uint) bool {
	t.Helper()
	d.stream++
	d.hub.RoutePacket(context.Background(), models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         1000001,
		Dst:         tg,
		Repeater:    999998,
		GroupCall:   true,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA,
		StreamID:    d.stream,
		BER:         -1,
		RSSI:        -1,
	}, models.RepeaterTypeMMDVM)
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case rp := <-d.handle.Packets:
			if rp.RepeaterID == repeaterID && rp.Packet.Dst == tg {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
