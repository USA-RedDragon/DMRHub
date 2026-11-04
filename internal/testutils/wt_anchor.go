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

import "github.com/USA-RedDragon/DMRHub/internal/db/models"

// anchorGap is how many bursts of a stream after a missing one a browser
// hotspot takes before it gives the missing one up and moves its anchor
// on, so that a burst that never comes holds the anchor back by at most
// that many.
const anchorGap = 64

// Anchor is the last burst of a slot that a browser hotspot took with no
// burst of its stream missing before it, as it sends it to DMRHub, and
// First, the first burst of that stream that it took.
type Anchor struct {
	Slot   uint  `json:"slot"`
	Stream uint  `json:"stream"`
	Seq    uint  `json:"seq"`
	First  *uint `json:"first,omitempty"`
}

type slotAnchor struct {
	stream, last  uint
	seq, first    uint8
	ahead, behind map[uint8]bool
}

// anchors are the anchors of a browser hotspot on its two slots, as
// DMRHub Radio keeps them.
type anchors struct {
	slots [2]*slotAnchor
}

// take notes a burst that the hotspot took.
func (a *anchors) take(p models.Packet) {
	i := 0
	if p.Slot {
		i = 1
	}
	seq := uint8(p.Seq & 0xff) //nolint:gosec // the sequence number is one octet on the wire
	s := a.slots[i]
	switch {
	case s == nil || (p.StreamID != s.stream && p.StreamID != s.last):
		var last uint
		if s != nil {
			last = s.stream
		}
		a.slots[i] = &slotAnchor{stream: p.StreamID, last: last, seq: seq, first: seq, ahead: map[uint8]bool{}, behind: map[uint8]bool{}}
		return
	case p.StreamID != s.stream:
		return
	}
	d := seq - s.seq
	if d == 0 || d > 128 {
		if b := s.first - seq; b > 0 && b <= 128 {
			s.behind[seq] = true
			for s.behind[s.first-1] {
				s.first--
				delete(s.behind, s.first)
			}
		}
		return
	}
	s.ahead[seq] = true
	if d > anchorGap {
		to := seq - anchorGap
		for x := range s.ahead {
			if x-s.seq <= to-s.seq {
				delete(s.ahead, x)
			}
		}
		s.seq = to
	}
	for s.ahead[s.seq+1] {
		s.seq++
		delete(s.ahead, s.seq)
	}
}

// list returns the anchors of the slots on which the hotspot took a burst.
func (a *anchors) list() []Anchor {
	out := []Anchor{}
	for i, s := range a.slots {
		if s != nil {
			out = append(out, Anchor{Slot: uint(i) + 1, Stream: s.stream, Seq: uint(s.seq), First: new(uint(s.first))})
		}
	}
	return out
}
