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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
)

func tookBursts(a *anchors, stream uint, seqs ...uint) *anchors {
	for _, seq := range seqs {
		a.take(models.Packet{StreamID: stream, Seq: seq})
	}
	return a
}

// TestAnAnchorTellsTheFirstBurstThatTheHotspotTook takes a call from its
// fourth burst on. The anchor must tell that burst as the first that the
// hotspot took, so that a replica sends the three before it again.
func TestAnAnchorTellsTheFirstBurstThatTheHotspotTook(t *testing.T) {
	t.Parallel()
	a := tookBursts(&anchors{}, 7, 3, 4, 5)
	assert.Equal(t, []Anchor{{Slot: 1, Stream: 7, Seq: 5, First: new(uint(3))}}, a.list())
}

// TestTheFirstBurstMovesBackWithTheBurstsSentAgain takes a call from its
// fourth burst on, and then the three before it, as a replica sends them
// again. The first burst that the hotspot took with none missing after
// it is then the first of the call.
func TestTheFirstBurstMovesBackWithTheBurstsSentAgain(t *testing.T) {
	t.Parallel()
	a := tookBursts(&anchors{}, 7, 3, 4, 5, 0, 1)
	assert.Equal(t, []Anchor{{Slot: 1, Stream: 7, Seq: 5, First: new(uint(3))}}, a.list())
	tookBursts(a, 7, 2)
	assert.Equal(t, []Anchor{{Slot: 1, Stream: 7, Seq: 5, First: new(uint(0))}}, a.list())
}

// TestAnAnchorStartsAgainAtANewStream takes two calls. The first burst
// must be that of the second call.
func TestAnAnchorStartsAgainAtANewStream(t *testing.T) {
	t.Parallel()
	a := tookBursts(&anchors{}, 7, 0, 1, 2)
	tookBursts(a, 8, 5, 6)
	assert.Equal(t, []Anchor{{Slot: 1, Stream: 8, Seq: 6, First: new(uint(5))}}, a.list())
}
