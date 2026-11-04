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
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
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
// holds the timeslot until its terminator, or until StreamIdle passes with
// no burst of it; the other group calls to the timeslot are dropped
// meanwhile, to their end.
func (h *Hub) holdsSlot(repeaterID uint, slot bool, packet models.Packet) bool {
	if h.routingConfig().Contention != config.ContentionFirst {
		return true
	}
	key := repeaterSlot{repeater: repeaterID, slot: slot}
	mine := slotStream{repeaterSlot: key, stream: packet.StreamID, src: packet.Src}
	now := h.clock.Now()
	if _, ok := h.slotRefused.Load(mine); ok {
		h.slotRefused.Store(mine, now)
		return false
	}
	end := packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm
	held, logIt := true, false
	var holder slotHolder
	h.slotHolders.Compute(key, func(cur slotHolder, loaded bool) (slotHolder, xsync.ComputeOp) {
		if loaded && (cur.stream != packet.StreamID || cur.src != packet.Src) && now.Sub(cur.last) < StreamIdle {
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

// keyUp is whether the first burst of a stream came in while another
// call held the timeslot that the stream came in on.
type keyUp struct {
	busy bool
	last time.Time
}

// keyedOnBusySlot queues "Channel busy." for the radio of the group burst
// packet from sourceName when the first burst of its stream came in while
// another call held the timeslot of its repeater under the first
// contention setting. The burst is routed either way.
func (h *Hub) keyedOnBusySlot(packet models.Packet, sourceName string) {
	if !packet.GroupCall || packet.StreamID == 0 || !h.promptsOn() ||
		h.routingConfig().Contention != config.ContentionFirst || !h.promptable(packet, sourceName) {
		return
	}
	now := h.clock.Now()
	key := claimKey{stream: packet.StreamID, src: packet.Src}
	k, _ := h.keyUps.LoadOrCompute(key, func() (keyUp, bool) {
		return keyUp{busy: h.heldByOther(repeaterSlot{repeater: packet.Repeater, slot: packet.Slot}, packet, now)}, false
	})
	k.last = now
	h.keyUps.Store(key, k)
	if k.busy {
		h.promptRefused(packet, sourceName, prompts.ChannelBusy)
	}
}

// heldByOther reports whether a call other than the stream of packet holds
// the timeslot key at now.
func (h *Hub) heldByOther(key repeaterSlot, packet models.Packet, now time.Time) bool {
	cur, ok := h.slotHolders.Load(key)
	return ok && (cur.stream != packet.StreamID || cur.src != packet.Src) && now.Sub(cur.last) < StreamIdle
}

func (h *Hub) pruneSlotHolders(now time.Time) {
	h.keyUps.Range(func(k claimKey, v keyUp) bool {
		if now.Sub(v.last) > sightedStreamTTL {
			h.keyUps.Delete(k)
		}
		return true
	})
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
