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

import (
	"context"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/require"
)

// checkHandoverHistory sends two calls, one on each slot, to a repeater at
// ep of a replica that drains, 100 bursts in all, and then hands the
// repeater over from before the first burst, as when the move reached the
// replica late on a starved machine. The handover must hold every burst.
func checkHandoverHistory(t *testing.T, ep endpoint) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		s := queueServer()
		s.connected.Store(1, ep)
		h := hub.NewHub(nil, nil, nil, nil)
		defer h.Stop(context.Background())
		s.hub = h
		s.config = &config.Config{}
		s.config.DMR.DrainTimeout = 25
		h.MarkLeaving()
		since := time.Now()
		time.Sleep(time.Millisecond)
		const bursts = 100
		for i := range bursts {
			s.deliver(context.Background(), 1, models.Packet{Slot: i%2 == 1, StreamID: 7 + uint(i%2), Seq: uint(i / 2)})
			s.writeOut(<-s.outgoingChan)
			time.Sleep(30 * time.Millisecond)
		}
		got := s.sentSince(1, since, ruleFor(ep))
		require.Equal(t, bursts, len(got), "the handover lost bursts that left after the move")
	})
}

// TestHandOverEveryBurstOfASlowMove is checkHandoverHistory for an MMDVM
// repeater.
func TestHandOverEveryBurstOfASlowMove(t *testing.T) {
	t.Parallel()
	checkHandoverHistory(t, udpEndpoint(net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}))
}

// TestHandOverEveryBurstOfASlowBrowserMove is checkHandoverHistory for a
// browser hotspot.
func TestHandOverEveryBurstOfASlowBrowserMove(t *testing.T) {
	t.Parallel()
	checkHandoverHistory(t, endpoint{transport: webTransportName, addr: wtAddr("a")})
}

// TestHandOverWhatWaitsForAnEarlierHandover moves a repeater on while the
// replica that it moved to still holds bursts for it until the handover of
// the replica before comes. Those bursts never went to the repeater, so
// the handover to the next replica must hold them, after what this one
// sent, in order.
func TestHandOverWhatWaitsForAnEarlierHandover(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := queueServer()
		since := time.Now()
		time.Sleep(time.Millisecond)
		s.deliver(context.Background(), 1, models.Packet{StreamID: 7, Seq: 1})
		s.writeOut(<-s.outgoingChan)
		s.moving = map[uint]*movingRepeater{1: {seen: map[packetKey]bool{}}}
		for _, seq := range []uint{2, 3} {
			require.True(t, s.hold(1, models.Packet{StreamID: 7, Seq: seq}))
		}
		got := s.sentSince(1, since, ruleFor(udpEndpoint(net.UDPAddr{})))
		seqs := make([]uint, 0, len(got))
		for _, p := range got {
			seqs = append(seqs, p.Seq)
		}
		require.Equal(t, []uint{1, 2, 3}, seqs, "the handover lost the bursts that waited for the earlier one")
	})
}
