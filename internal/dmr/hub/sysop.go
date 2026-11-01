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
	"encoding/json"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
)

const (
	repeaterResetTopic = "hub:repeaters:reset"
	callDropTopic      = "hub:calls:drop"
)

// OnRepeaterReset calls fn with the ID of a repeater after RepeaterReset
// on any replica. The returned func stops the calls.
func (h *Hub) OnRepeaterReset(fn func(repeaterID uint)) (stop func()) {
	return h.watchers.add(repeaterResetTopic, fn)
}

// RepeaterReset tells every replica to end the sessions of the repeater
// id, as a sysop asked, so that it logs in again.
func (h *Hub) RepeaterReset(id uint) {
	h.announce(repeaterResetTopic, id)
}

type callDrop struct {
	Origin   string `json:"origin"`
	Repeater uint   `json:"repeater"`
	Slot     bool   `json:"slot"`
	Stream   uint   `json:"stream"`
	Src      uint   `json:"src"`
	Dst      uint   `json:"dst"`
}

type dropRecord struct {
	stream, src, dst uint
	ended            bool
	last             time.Time
}

func (d dropRecord) matches(p *models.Packet) bool {
	return p.StreamID == d.stream || (p.Src == d.src && p.Dst == d.dst)
}

// DropCall makes every replica stop carrying the call of stream, from src
// to dst, on the timeslot slot of the repeater, in both directions, until
// it ends: the hub turns the next burst of the call into a terminator, so
// that the radios unmute at once, and drops the rest of it.
func (h *Hub) DropCall(repeaterID uint, slot bool, stream, src, dst uint) {
	d := callDrop{Origin: h.origin, Repeater: repeaterID, Slot: slot, Stream: stream, Src: src, Dst: dst}
	h.applyDrop(d)
	if h.pubsub == nil {
		return
	}
	b, err := json.Marshal(d)
	if err != nil {
		slog.Error("Failed to encode a dropped call", "error", err)
		return
	}
	if err := h.pubsub.Publish(callDropTopic, b); err != nil {
		slog.Error("Failed to announce a dropped call", "repeaterID", repeaterID, "error", err)
	}
}

func (h *Hub) applyDrop(d callDrop) {
	h.drops.Store(repeaterSlot{repeater: d.Repeater, slot: d.Slot}, dropRecord{stream: d.Stream, src: d.Src, dst: d.Dst, last: h.clock.Now()})
}

func (h *Hub) listenDrops(sub pubsub.Subscription) {
	defer func() {
		if err := sub.Close(); err != nil {
			slog.Error("Error closing the dropped call subscription", "error", err)
		}
	}()
	ch := sub.Channel()
	for {
		select {
		case <-h.done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var d callDrop
			if err := json.Unmarshal(msg, &d); err != nil || d.Origin == h.origin {
				continue
			}
			h.applyDrop(d)
		}
	}
}

func (h *Hub) dropped(repeaterID uint, slot bool, packet models.Packet) (out models.Packet, carry bool) {
	key := repeaterSlot{repeater: repeaterID, slot: slot}
	d, ok := h.drops.Load(key)
	if !ok || !d.matches(&packet) {
		return packet, true
	}
	if isTerminator(&packet) {
		h.drops.Delete(key)
		return packet, !d.ended
	}
	d.last = h.clock.Now()
	if d.ended {
		h.drops.Store(key, d)
		return packet, false
	}
	d.ended = true
	h.drops.Store(key, d)
	return terminatorOf(packet), true
}

func (h *Hub) dropsIngress(packet *models.Packet, sourceName string) bool {
	if h.getServerRole(sourceName) == RolePeer {
		return false
	}
	out, carry := h.dropped(packet.Repeater, packet.Slot, *packet)
	*packet = out
	return !carry
}

func (h *Hub) pruneDrops(now time.Time) {
	h.drops.Range(func(k repeaterSlot, d dropRecord) bool {
		if now.Sub(d.last) > StreamIdle {
			h.drops.Delete(k)
		}
		return true
	})
}

func isTerminator(p *models.Packet) bool {
	return p.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(p.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm
}

func terminatorOf(p models.Packet) models.Packet {
	lc, err := pdu.NewVoiceLCBytes(p.GroupCall, uint32(p.Dst), uint32(p.Src), layer3.ServiceOptions{}) //nolint:gosec
	if err != nil {
		return p
	}
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: layer2.SyncSourceBS})
	if err != nil {
		return p
	}
	p.FrameType = dmrconst.FrameDataSync
	p.DTypeOrVSeq = uint(dmrconst.DTypeVoiceTerm)
	p.DMRData = vt.Terminator()
	return p
}
