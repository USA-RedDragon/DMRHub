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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/stretchr/testify/require"
)

func dropBurst(stream, src, dst uint, term bool) models.Packet {
	p := models.Packet{Src: src, Dst: dst, GroupCall: true, StreamID: stream, FrameType: 1, DTypeOrVSeq: 0}
	if term {
		return terminatorOf(p)
	}
	return p
}

// TestADroppedCallEndsWithATerminatorAndStops drops a call on TS2 of a
// repeater and hands the hub its bursts: the first becomes the terminator
// of the call with its LC, the rest are dropped, a burst of another call
// or timeslot goes on, and the real terminator ends the drop.
func TestADroppedCallEndsWithATerminatorAndStops(t *testing.T) {
	t.Parallel()
	h := NewHub(nil, nil, nil, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })
	h.DropCall(100001, true, 7601, 1000001, 3100)

	out, carry := h.dropped(100001, true, dropBurst(7601, 1000001, 3100, false))
	require.True(t, carry)
	require.True(t, isTerminator(&out), "the first burst after the drop is a terminator")
	raw := layer2.RawBurst(out.DMRData)
	lc, ok := raw.MMDVMHostPayload(enums.DataTypeTerminatorWithLC)
	require.True(t, ok, "the terminator has a good LC")
	l := pdu.LCBytes(lc[:9])
	require.Equal(t, enums.FLCOGroupVoiceChannelUser, l.FLCO())

	_, carry = h.dropped(100001, true, dropBurst(7601, 1000001, 3100, false))
	require.False(t, carry, "the next burst is dropped")
	_, carry = h.dropped(100001, true, dropBurst(9999, 1000001, 3100, false))
	require.False(t, carry, "a copy of the call with another stream ID is dropped")
	_, carry = h.dropped(100001, false, dropBurst(7601, 1000001, 3100, false))
	require.True(t, carry, "the other timeslot is not dropped")
	_, carry = h.dropped(100002, true, dropBurst(7601, 1000001, 3100, false))
	require.True(t, carry, "another repeater is not dropped")
	_, carry = h.dropped(100001, true, dropBurst(7602, 1000002, 91, false))
	require.True(t, carry, "another call is not dropped")

	_, carry = h.dropped(100001, true, dropBurst(7601, 1000001, 3100, true))
	require.False(t, carry, "the real terminator comes after the hub ended the call")
	out, carry = h.dropped(100001, true, dropBurst(7601, 1000001, 3100, false))
	require.True(t, carry, "the drop ends with the call")
	require.False(t, isTerminator(&out))
}

// TestADropOfACallThatEndsAloneIsForgotten drops a call whose next burst
// is its terminator, and a call that sends nothing more: the first ends
// with that terminator, the second is forgotten StreamIdle later.
func TestADropOfACallThatEndsAloneIsForgotten(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	h := NewHub(nil, nil, nil, nil, WithClock(fake))
	t.Cleanup(func() { h.Stop(context.Background()) })

	h.DropCall(100001, false, 7601, 1000001, 3100)
	_, carry := h.dropped(100001, false, dropBurst(7601, 1000001, 3100, true))
	require.True(t, carry, "a terminator that the hub did not send goes on")
	_, ok := h.drops.Load(repeaterSlot{repeater: 100001, slot: false})
	require.False(t, ok)

	h.DropCall(100001, false, 7602, 1000001, 3100)
	fake.Advance(StreamIdle)
	h.pruneDrops(fake.Now())
	_, ok = h.drops.Load(repeaterSlot{repeater: 100001, slot: false})
	require.True(t, ok, "the drop waits StreamIdle")
	fake.Advance(time.Millisecond)
	h.pruneDrops(fake.Now())
	_, ok = h.drops.Load(repeaterSlot{repeater: 100001, slot: false})
	require.False(t, ok, "the drop is forgotten after StreamIdle with no burst")
}
