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

package mmdvm

import "github.com/USA-RedDragon/DMRHub/internal/db/models"

// wtAnchor is the anchor of a browser hotspot on one slot: the last burst
// of the slot that it took with no burst of its stream missing before it.
// A hotspot that knows what it got sends its anchors to the replica that
// takes it over, and to the replica whose path to it moved, which then
// sends it every burst that went to it after them, rather than those of a
// time window, which a load balancer that stalls can outlast.
type wtAnchor struct {
	Slot   uint `json:"slot"`
	Stream uint `json:"stream"`
	Seq    uint `json:"seq"`
}

// slot2 reports whether the anchor is on slot 2.
func (a wtAnchor) slot2() bool {
	return a.Slot == 2
}

// afterAnchors returns the packets of sent, which went to a browser
// hotspot in that order, that came after the anchors of the hotspot, in
// order. Of a slot whose anchor is in sent, these are the packets after
// its last copy there; of a slot with no anchor, or whose anchor is older
// than sent, all of them, as the hotspot took none of them.
func afterAnchors(sent []models.Packet, anchors []wtAnchor) []models.Packet {
	from := map[bool]int{}
	for _, a := range anchors {
		if a.Slot != 1 && a.Slot != 2 {
			continue
		}
		for i := len(sent) - 1; i >= 0; i-- {
			p := sent[i]
			if p.Slot == a.slot2() && p.StreamID == a.Stream && p.Seq&0xff == a.Seq&0xff {
				from[p.Slot] = i + 1
				break
			}
		}
	}
	out := make([]models.Packet, 0, len(sent))
	for i, p := range sent {
		if i >= from[p.Slot] {
			out = append(out, p)
		}
	}
	return out
}
