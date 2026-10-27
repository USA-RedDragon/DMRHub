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

package hub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// HoldSource keeps the routing worker of the source slot of packet from
// sourceName busy until release is called.
func (h *Hub) HoldSource(packet models.Packet, sourceName string) (release func()) {
	ch := make(chan struct{})
	h.routing.Do(context.Background(), h.sourceSlotOf(packet, sourceName), func() { <-ch })
	return func() { close(ch) }
}

func (h *Hub) SwitchDynamicTalkgroup(ctx context.Context, packet models.Packet) {
	h.switchDynamicTalkgroup(ctx, packet)
}

// ForgetCachedRepeater drops the cached repeater, so that the next delivery
// to it reads the database.
func (h *Hub) ForgetCachedRepeater(id uint) {
	h.repeaters.drop(id)
}

// AdmitStream reports whether the hub takes packet in, waiting for the
// answer of admitStream.
func (h *Hub) AdmitStream(ctx context.Context, packet models.Packet, sourceName string) bool {
	answer := make(chan bool, 1)
	h.admitStream(ctx, packet, sourceName, func(ok bool) { answer <- ok })
	return <-answer
}

// SetStreamClaimTiming replaces streamClaimHold and streamClaimRecheck.
func (h *Hub) SetStreamClaimTiming(hold, recheck time.Duration) {
	h.claimTiming.Store(&[2]time.Duration{hold, recheck})
}

// HoldStreamClaimWaits makes the waits of the hub for the answers of the
// KV store for the claims of streams end only when end is called, instead
// of after streamClaimWait.
func (h *Hub) HoldStreamClaimWaits() (end func()) {
	ch := make(chan time.Time)
	h.claimWaitEnd.Store(&ch)
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// StreamClaimHold is streamClaimHold.
const StreamClaimHold = streamClaimHold

// StreamClaimWait is streamClaimWait.
const StreamClaimWait = streamClaimWait

// LiftSlotRate lets the test send faster than one repeater slot can, or
// limits it again.
func (h *Hub) LiftSlotRate(lift bool) {
	h.slotRateOff.Store(lift)
}

// HeardAt returns the repeater where private traffic in the hybrid and
// members reach follows the radio userID to, as the KV store keeps it, or
// 0.
func (h *Hub) HeardAt(ctx context.Context, userID uint) uint {
	v, err := h.kv.Get(ctx, lastHeardKey(userID))
	if err != nil {
		return 0
	}
	var rec lastHeard
	if json.Unmarshal(v, &rec) != nil {
		return 0
	}
	return rec.Vetted.Repeater
}

// ResyncRepeater runs one resync of the repeater id, and reports whether
// it reloaded the subscriptions.
func (h *Hub) ResyncRepeater(id uint) bool {
	return h.resyncRepeater(id)
}

// OutboxDepth is the number of bursts in the outboxes.
func (h *Hub) OutboxDepth() int64 {
	return h.outbox.Depth()
}

// StoreSighting adds to the record in the KV store that repeater heard
// radio at, as a sighting that private traffic in the hybrid and members
// reach may follow when vetted is set.
func (h *Hub) StoreSighting(radio, repeater uint, at time.Time, vetted bool) error {
	return h.storeSighting(sighting{src: radio, repeater: repeater, at: at}, lastHeardKey(radio), vetted)
}
