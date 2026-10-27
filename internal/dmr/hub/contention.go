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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/puzpuzpuz/xsync/v4"
)

// slotHolder is the group call that holds a timeslot of a repeater under
// the first contention setting.
type slotHolder struct {
	stream uint
	src    uint
	last   time.Time
}

// slotStream is a stream of a radio to a timeslot of a repeater.
type slotStream struct {
	repeaterSlot
	stream, src uint
}

// holdsSlot reports whether the group burst packet may go to the timeslot
// slot of the repeater. Under the first contention setting, the first call
// holds the timeslot until its terminator, or until streamIdle passes with
// no burst of it; the other group calls to the timeslot are dropped
// meanwhile, to their end.
func (h *Hub) holdsSlot(repeaterID uint, slot bool, packet models.Packet) bool {
	if h.routingConfig().Contention != config.ContentionFirst {
		return true
	}
	key := repeaterSlot{repeater: repeaterID, slot: slot}
	mine := slotStream{repeaterSlot: key, stream: packet.StreamID, src: packet.Src}
	now := time.Now()
	if _, ok := h.slotRefused.Load(mine); ok {
		h.slotRefused.Store(mine, now)
		return false
	}
	end := packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm
	held, logIt := true, false
	var holder slotHolder
	h.slotHolders.Compute(key, func(cur slotHolder, loaded bool) (slotHolder, xsync.ComputeOp) {
		if loaded && (cur.stream != packet.StreamID || cur.src != packet.Src) && now.Sub(cur.last) < streamIdle {
			held, holder = false, cur
			logIt = true
			return cur, xsync.CancelOp
		}
		if end {
			return cur, xsync.DeleteOp
		}
		return slotHolder{stream: packet.StreamID, src: packet.Src, last: now}, xsync.UpdateOp
	})
	if !held {
		h.slotRefused.Store(mine, now)
	}
	if logIt {
		slog.Info("Dropping a group call to a timeslot that another call holds",
			"repeaterID", repeaterID, "slot2", slot, "stream", packet.StreamID, "src", packet.Src, "dst", packet.Dst,
			"holderStream", holder.stream, "holderSrc", holder.src)
	}
	return held
}

func (h *Hub) pruneSlotHolders(now time.Time) {
	h.slotRefused.Range(func(k slotStream, at time.Time) bool {
		if now.Sub(at) > sightedStreamTTL {
			h.slotRefused.Delete(k)
		}
		return true
	})
	h.slotHolders.Range(func(k repeaterSlot, v slotHolder) bool {
		if now.Sub(v.last) > sightedStreamTTL {
			h.slotHolders.Delete(k)
		}
		return true
	})
}
