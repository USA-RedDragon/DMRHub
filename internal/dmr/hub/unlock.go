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
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/undeliverable"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
)

const (
	unlockChangedTopic = "hub:unlocks:changed"
	// unlockWork is the work of laterKey that checks on-air codes.
	unlockWork = "unlock"
)

// Unlock names the radio ID Src that sent on the slot of the repeater or
// hotspot Repeater, through the server Server. It never holds a code.
type Unlock struct {
	Server   string
	Repeater uint
	Src      uint
	Slot2    bool
}

// UnlockReplies answers a radio about its on-air code, such as with a
// spoken prompt. The hub calls Locked once for each transmission of a
// radio ID that needs an unlock on its repeater slot, and Tried after each
// try of a code. With no UnlockReplies set, the radio hears nothing.
type UnlockReplies interface {
	Locked(u Unlock)
	Tried(u Unlock, r airauth.Result)
}

type unlockStream struct {
	server   string
	repeater uint
	slot     bool
	stream   uint
}

func (h *Hub) initUnlocks() {
	h.airAuth = airauth.New(h.db, h.kv, h.clock, h.unlockKey, func() time.Duration { return h.routingConfig().OnAirUnlock })
	h.unlockGrants = lookup.New(h.clock, "unlocks", lookup.TTL, func(radio uint) ([]airauth.Grant, error) {
		if h.kv == nil {
			return nil, nil
		}
		return h.airAuth.Grants(context.Background(), radio)
	})
}

// loadOnAirRadios reads again the radio IDs of the users with the on-air
// code on. On an error it keeps the radio IDs that it had.
func (h *Hub) loadOnAirRadios() {
	if h.db == nil {
		return
	}
	radios, err := models.OnAirRadios(h.db)
	if err != nil {
		slog.Error("Cannot read the radio IDs of the on-air codes, keeping the old ones", "error", err)
		return
	}
	h.onAirRadios.Store(&radios)
}

// runOnAirReload reads the radio IDs of the on-air codes again every
// lookup.TTL, so that a replica that missed the notice of a change locks
// or frees a radio ID within that time.
func (h *Hub) runOnAirReload() {
	ticker := h.clock.NewTicker(lookup.TTL)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C():
			h.loadOnAirRadios()
		}
	}
}

// onAirOwner returns the user of the radio ID radio when that user has the
// on-air code on, and whether the hub has read those radio IDs yet.
func (h *Hub) onAirOwner(radio uint) (owner uint, known bool) {
	m := h.onAirRadios.Load()
	if m == nil {
		return 0, false
	}
	return (*m)[radio], true
}

func (h *Hub) unlockKey() []byte {
	if src := h.cfg.Load(); src != nil {
		return airauth.KeyOf((*src).Get())
	}
	return nil
}

// SetUnlockClock makes the on-air codes read the time from c, as tests do
// to hold a time step still. Call it before the hub takes traffic.
func (h *Hub) SetUnlockClock(c clock.Clock) {
	h.airAuth = airauth.New(h.db, h.kv, c, h.unlockKey, func() time.Duration { return h.routingConfig().OnAirUnlock })
}

// SetUnlockReplies makes r answer the radios about their on-air codes.
func (h *Hub) SetUnlockReplies(r UnlockReplies) {
	h.unlockReplies.Store(&r)
}

// OnAirCodes returns the on-air codes of the hub.
func (h *Hub) OnAirCodes() *airauth.Service {
	return h.airAuth
}

// UnlocksChanged makes every replica read the unlocks of the radio ID
// radio again. Call it after an unlock of the radio ends early.
func (h *Hub) UnlocksChanged(radio uint) {
	h.announce(unlockChangedTopic, radio)
}

// OnUnlocksChange calls fn with a radio ID after this replica reads its
// unlocks again for an UnlocksChanged on any replica. The returned func
// stops the calls.
func (h *Hub) OnUnlocksChange(fn func(radio uint)) (stop func()) {
	return h.watchers.add(unlockChangedTopic, fn)
}

// takeUnlockCall takes packet in when it carries an on-air code, before
// any other part of the hub sees it, and reports whether it did. The first
// burst of a voice call from a repeater or hotspot queues one check of the
// code. Nothing records, publishes or logs the destination of the call.
// Calls from peers and plugins to the codes are dropped.
func (h *Hub) takeUnlockCall(ctx context.Context, packet models.Packet, sourceName string) bool {
	if !packet.IsUnlockCall() {
		return false
	}
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin || h.getServerRole(sourceName) == RolePeer {
		return true
	}
	if isVoice, _ := utils.CheckPacketType(packet); !isVoice {
		return true
	}
	key := unlockStream{server: sourceName, repeater: packet.Repeater, slot: packet.Slot, stream: packet.StreamID}
	if _, seen := h.unlockStreams.LoadOrStore(key, h.clock.Now()); seen {
		return true
	}
	code, _ := airauth.CodeOfCall(packet.Dst)
	u := Unlock{Server: sourceName, Repeater: packet.Repeater, Src: packet.Src, Slot2: packet.Slot}
	ctx = context.WithoutCancel(ctx)
	h.later.Add(laterKey{what: unlockWork, id: packet.Repeater}, outbox.Job{Topic: unlockWork, Send: func() error {
		var r airauth.Result
		owner, known := h.onAirOwner(u.Src)
		if !known {
			slog.Warn("On-air code try could not finish", "src", u.Src, "error", "the radio IDs of the on-air codes are not read yet")
			r = airauth.Unavailable
		} else {
			r = h.airAuth.Attempt(ctx, airauth.Try{Src: u.Src, Owner: owner, Code: code, Repeater: u.Repeater, Slot2: u.Slot2})
		}
		if r == airauth.Granted {
			h.UnlocksChanged(u.Src)
		}
		if f := h.unlockReplies.Load(); f != nil {
			(*f).Tried(u, r)
		}
		return nil
	}})
	return true
}

// needsUnlock reports whether packet comes from a radio ID whose owner
// turned the on-air code on and that no unlock lets through its repeater
// slot. The radio IDs of the on-air codes are in memory, so a radio ID of
// a user without the code needs no read of the database or the KV store.
// Before the hub has read them once, no radio ID needs an unlock. When the
// hub cannot read yet the unlocks of a radio ID, it needs one.
func (h *Hub) needsUnlock(packet models.Packet) bool {
	if owner, _ := h.onAirOwner(packet.Src); owner == 0 {
		return false
	}
	grants, err := h.unlockGrants.Get(packet.Src)
	if err != nil {
		metrics.NewMetrics().IncrementIngressDropped(metrics.DropReasonLookupWait)
		return true
	}
	return !airauth.Unlocked(grants, packet.Repeater, packet.Slot, h.clock.Now())
}

// refuseLocked refuses packet when its radio ID needs an unlock, and tells
// the UnlockReplies once for each of its transmissions.
func (h *Hub) refuseLocked(packet models.Packet, sourceName string) bool {
	if !h.needsUnlock(packet) {
		return false
	}
	h.ReportUndeliverable(packet, sourceName, undeliverable.ReasonUnlockNeeded)
	key := unlockStream{server: sourceName, repeater: packet.Repeater, slot: packet.Slot, stream: packet.StreamID}
	if _, told := h.lockedStreams.LoadOrStore(key, h.clock.Now()); !told {
		if f := h.unlockReplies.Load(); f != nil {
			(*f).Locked(Unlock{Server: sourceName, Repeater: packet.Repeater, Src: packet.Src, Slot2: packet.Slot})
		}
	}
	return true
}
