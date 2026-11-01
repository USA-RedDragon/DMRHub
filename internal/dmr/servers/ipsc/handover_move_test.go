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
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rtpLog records the RTP sequence numbers of the frames that a pacer sends.
type rtpLog struct {
	seqs chan uint16
}

func (l rtpLog) send(_ slotKey, frame []byte) {
	if u, err := ipscwire.DecodeUser(frame); err == nil {
		l.seqs <- u.RTP.Seq
	}
}

func (l rtpLog) take(n int) []uint16 {
	out := make([]uint16, 0, n)
	for range n {
		out = append(out, <-l.seqs)
	}
	return out
}

// TestAMoveRightAfterALateHandoverKeepsTheRTPSequence makes a replica go on
// with a call to a peer from the state of a handover, as after a move, and
// send its packets, late, just before the peer moves on to a third replica.
// The third replica gets them in the handover and sends them again; the
// peer must get them with the RTP sequence numbers that it got them with
// before, not as a new call from zero.
func TestAMoveRightAfterALateHandoverKeepsTheRTPSequence(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const id, first, n = uint32(311860), 36, 6
		fromB, fromC := rtpLog{seqs: make(chan uint16, n)}, rtpLog{seqs: make(chan uint16, n)}
		b := &IPSCServer{clock: clock.Real(), pacer: newPacer(clock.Real(), fromB.send, nil), localID: 1}
		t.Cleanup(b.pacer.stop)
		b.egressTranslator(id).Continue(7, streamSnapshot{CallControl: 9, RTPSeq: 100, RTPTimestamp: 48000, CallSeq: 3})
		for i := first; i < first+n; i++ {
			b.sendTo(id, handedVoice(i))
		}
		sentByB := fromB.take(n)
		require.Equal(t, uint16(100), sentByB[0])

		calls, lost, _ := b.pacer.handOver(id, b.clock.Now())
		require.Len(t, lost, n, "the handover holds the packets sent just before the move")
		c := &IPSCServer{clock: clock.Real(), pacer: newPacer(clock.Real(), fromC.send, nil), localID: 2, moving: map[uint32]*moving{id: newMoving()}}
		t.Cleanup(c.pacer.stop)
		c.finishHandover(id, c.clock.Now(), &handoverReport{Calls: calls, Lost: marshalPackets(nil, lost)})
		assert.Equal(t, sentByB, fromC.take(n), "the next replica sent the packets of the handover as a new call")
	})
}
