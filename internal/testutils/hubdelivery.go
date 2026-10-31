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
// does not exist and reports whether the hub delivered it to repeaterID
// within 500 ms. Use it only to check that a burst does not arrive: a slow
// machine can only hide a wrong delivery from it. RequireGroupBurst checks
// that one does.
func (d *HubDelivery) GroupBurstReaches(t *testing.T, tg, repeaterID uint) bool {
	t.Helper()
	d.route(tg)
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

// DeliveryWait bounds the wait of RequireGroupBurst. The hub delivers a
// burst at once when it works, so the bound only ends a test that fails.
const DeliveryWait = time.Minute

// RequireGroupBurst routes one group voice burst to tg from a repeater that
// does not exist and fails t unless the hub delivers it to repeaterID
// within DeliveryWait.
func (d *HubDelivery) RequireGroupBurst(t *testing.T, tg, repeaterID uint) {
	t.Helper()
	d.route(tg)
	ctx, cancel := context.WithTimeout(t.Context(), DeliveryWait)
	defer cancel()
	for {
		select {
		case rp := <-d.handle.Packets:
			if rp.RepeaterID == repeaterID && rp.Packet.Dst == tg {
				return
			}
		case <-ctx.Done():
			t.Fatalf("the hub did not deliver a burst of talkgroup %d to repeater %d within %s", tg, repeaterID, DeliveryWait)
		}
	}
}

func (d *HubDelivery) route(tg uint) {
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
}
