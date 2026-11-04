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
	h.switchDynamicTalkgroup(ctx, packet, h.clock.Now(), h.clock.Now(), h.sessionEpoch(packet.Repeater))
}

// ExpireDynamicAfter unlinks tg from the timeslot of the repeater as the
// timeout sweep does, and calls between after the sweep read the
// repeater and before it writes.
func (h *Hub) ExpireDynamicAfter(id uint, slot bool, tg uint, between func()) {
	h.unlinkDynamicIfAfter(id, slot, tg, between)
}

// ForgetCachedRepeater drops the cached repeater, so that the next delivery
// to it reads the database.
func (h *Hub) ForgetCachedRepeater(id uint) {
	h.repeaters.drop(id)
}

// RoutingRepeater returns the repeater id as the delivery path reads it.
func (h *Hub) RoutingRepeater(id uint) (*models.Repeater, error) {
	return h.routingRepeater(id)
}

// ReloadCachedRepeater reads the repeater id again into the cache, as
// after a change of it.
func (h *Hub) ReloadCachedRepeater(id uint) {
	h.reloadRepeater(id)
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
	return h.heardRecord(ctx, userID).Vetted.Repeater
}

// HeardLastOn returns the last repeater that heard the radio, as the KV
// store keeps it, or 0.
func (h *Hub) HeardLastOn(ctx context.Context, radio uint) uint {
	return h.heardRecord(ctx, radio).Any.Repeater
}

func (h *Hub) heardRecord(ctx context.Context, radio uint) lastHeard {
	var rec lastHeard
	v, err := h.kv.Get(ctx, lastHeardKey(radio))
	if err != nil || json.Unmarshal(v, &rec) != nil {
		return lastHeard{}
	}
	return rec
}

// HeardHere returns the repeater where this hub heard the radio in the
// last seconds, which the record in the KV store may not hold yet, or 0.
func (h *Hub) HeardHere(radio uint) uint {
	heard, _ := h.heardOn.Load(radio)
	return heard.repeater
}

// LookupsLoaded reports whether the hub holds the peers, whether each of
// talkgroups exists and the owner of each of radios, so that no burst
// waits on the database for them. It starts the loads that are missing,
// and waits for each at most lookup.Wait.
func (h *Hub) LookupsLoaded(talkgroups, radios []uint) bool {
	if _, err := h.egressTable.Get(struct{}{}); err != nil {
		return false
	}
	for _, tg := range talkgroups {
		if _, err := h.talkgroupIDs.Get(tg); err != nil {
			return false
		}
	}
	for _, radio := range radios {
		if _, err := h.radioOwners.Get(radio); err != nil {
			return false
		}
	}
	return true
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

// WaitBackground waits until the background work of the hub is done.
func (h *Hub) WaitBackground() {
	h.later.Wait()
}

// ForgetOwnerAnswers drops the answers that the hub keeps for the owner of
// the radio ID radio and for its unlocks, as when they were idle too long.
func (h *Hub) ForgetOwnerAnswers(radio uint) {
	h.radioOwners.Drop(radio)
	h.unlockGrants.Drop(radio)
}

// PromptQueued reports whether a prompt waits to play on the slot of the
// repeater.
func (h *Hub) PromptQueued(repeater uint, slot2 bool) bool {
	s := &h.promptState
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.pending {
		if k.repeater == repeater && k.slot == slot2 {
			return true
		}
	}
	return false
}
