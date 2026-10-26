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

package ipsc

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sentPacer(since time.Time, before ...time.Duration) *pacer {
	p := newPacer(func(slotKey, []byte) {}, nil)
	q := &slotQueue{discarded: map[uint]time.Time{}, wake: make(chan struct{}, 1)}
	for i, d := range before {
		q.sent = append(q.sent, sentPacket{
			at:    since.Add(-d),
			pkt:   models.Packet{StreamID: 7, Seq: uint(i)},
			state: streamSnapshot{CallControl: 1, RTPSeq: uint16(i + 1)},
		})
	}
	p.slots[slotKey{peer: 1}] = q
	return p
}

// TestHandOverPacketsSentJustBeforeTheClaim sends packets to a peer 300 ms
// and a moment before the packet of the peer reached the other replica, as
// a load balancer that stalls under CPU throttling delays it. The load
// balancer moved the peer when it got that packet, earlier, and may have
// dropped them, so the handover must hold them, and the calls must go on
// from the state before them.
func TestHandOverPacketsSentJustBeforeTheClaim(t *testing.T) {
	t.Parallel()
	since := time.Now()
	p := sentPacer(since, 2*time.Second, 300*time.Millisecond, 100*time.Microsecond)
	calls, lost, _ := p.handOver(1, since)
	require.Len(t, lost, 2, "the handover misses packets sent just before the claim")
	assert.Equal(t, uint(1), lost[0].Seq)
	assert.Equal(t, uint(2), lost[1].Seq)
	require.Len(t, calls, 1)
	assert.Equal(t, uint(0), calls[0].Seq)
	assert.Equal(t, uint16(1), calls[0].State.RTPSeq)
}

// TestHandOverNoPacketSentWellBeforeTheClaim sends packets to a peer well
// before it moved. The peer has them, so the handover must not hold them.
func TestHandOverNoPacketSentWellBeforeTheClaim(t *testing.T) {
	t.Parallel()
	since := time.Now()
	p := sentPacer(since, 2*hub.MoveSlack+60*time.Millisecond, 2*hub.MoveSlack)
	calls, lost, _ := p.handOver(1, since)
	assert.Empty(t, lost)
	require.Len(t, calls, 1)
	assert.Equal(t, uint(1), calls[0].Seq)
}
