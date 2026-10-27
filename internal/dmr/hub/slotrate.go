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
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/puzpuzpuz/xsync/v4"
)

const (
	// slotRate is how many bursts a second one repeater slot may send. A
	// slot carries a burst every 60 ms, about 17 a second; three times that
	// leaves room for jitter and resends.
	slotRate = 50
	// slotBurst is how many bursts a slot may send at once.
	slotBurst = 50
	// slotIdle is how long a slot that sends nothing keeps its allowance.
	slotIdle = time.Minute
)

type slotKey struct {
	server   string
	repeater uint
	slot     bool
}

type slotBucket struct {
	tokens float64
	at     time.Time
	logged bool
}

// AdmitSlotRate reports whether the packet fits the rate of its repeater
// slot, at most slotRate bursts a second with slotBurst at once. A slot
// of a repeater cannot carry more, so more is a flood, which is dropped
// before it reaches the routing. Peers, such as OpenBridge, carry many
// streams in bulk and are not limited.
func (h *Hub) AdmitSlotRate(packet models.Packet, sourceName string) bool {
	if h.slotRateOff.Load() || h.getServerRole(sourceName) == RolePeer {
		return true
	}
	now := h.slotNow()
	key := slotKey{server: sourceName, repeater: packet.Repeater, slot: packet.Slot}
	admitted, warn := true, false
	h.slotBuckets.Compute(key, func(b slotBucket, loaded bool) (slotBucket, xsync.ComputeOp) {
		if !loaded {
			b = slotBucket{tokens: slotBurst, at: now}
		}
		b.tokens = min(slotBurst, b.tokens+now.Sub(b.at).Seconds()*slotRate)
		b.at = now
		if b.tokens < 1 {
			admitted = false
			warn = !b.logged
			b.logged = true
			return b, xsync.UpdateOp
		}
		b.tokens--
		b.logged = false
		return b, xsync.UpdateOp
	})
	if admitted {
		return true
	}
	metrics.NewMetrics().IncrementIngressDropped(metrics.DropReasonSlotRate)
	if warn {
		slog.Warn("Dropping bursts over the rate of a repeater slot",
			"server", sourceName, "repeaterID", packet.Repeater, "slot2", packet.Slot, "limit", slotRate)
	}
	return false
}

// SlotOverRate reports whether the slot of repeater on the server named
// sourceName has no allowance left, without taking any. The socket reader
// of a server drops such packets at once, so that a flood never fills the
// socket that other repeaters share. Only packets that AdmitSlotRate takes
// use the allowance, so packets that spoof a repeater cannot use it up.
func (h *Hub) SlotOverRate(sourceName string, repeater uint, slot bool) bool {
	if h.slotRateOff.Load() || h.getServerRole(sourceName) == RolePeer {
		return false
	}
	b, ok := h.slotBuckets.Load(slotKey{server: sourceName, repeater: repeater, slot: slot})
	if !ok {
		return false
	}
	return b.tokens+h.slotNow().Sub(b.at).Seconds()*slotRate < 1
}

func (h *Hub) slotNow() time.Time {
	if now := h.slotClock.Load(); now != nil {
		return (*now)()
	}
	return time.Now()
}

func (h *Hub) pruneSlotBuckets(now time.Time) {
	h.slotBuckets.Range(func(k slotKey, b slotBucket) bool {
		if now.Sub(b.at) > slotIdle {
			h.slotBuckets.Delete(k)
		}
		return true
	})
}
