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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sentPacer(since time.Time, before ...time.Duration) *pacer {
	p := newPacer(clock.Real(), func(slotKey, []byte) {}, nil)
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

// TestHandOverACallSentOnlyJustBeforeTheClaim sends a peer the first
// packets of a call from this replica only just before the packet of the
// peer reached the other replica, as when this replica sends what an
// earlier handover gave it late. The handover holds them all, and the
// call must go on from the state before the first of them, so that the
// other replica sends them again with the same RTP sequence numbers and
// not as a new call.
func TestHandOverACallSentOnlyJustBeforeTheClaim(t *testing.T) {
	t.Parallel()
	since := time.Now()
	p := newPacer(clock.Real(), func(slotKey, []byte) {}, nil)
	q := &slotQueue{discarded: map[uint]time.Time{}, wake: make(chan struct{}, 1)}
	for i := range 3 {
		q.sent = append(q.sent, sentPacket{
			at:     since.Add(-100 * time.Millisecond),
			pkt:    models.Packet{StreamID: 7, Seq: uint(36 + i)},
			before: streamSnapshot{CallControl: 5, RTPSeq: uint16(40 + i)},
			state:  streamSnapshot{CallControl: 5, RTPSeq: uint16(41 + i)},
		})
	}
	p.slots[slotKey{peer: 1}] = q
	calls, lost, _ := p.handOver(1, since)
	require.Len(t, lost, 3)
	require.Len(t, calls, 1, "the handover holds no state of the call, so the other replica starts it again")
	assert.Equal(t, uint(35), calls[0].Seq)
	assert.Equal(t, streamSnapshot{CallControl: 5, RTPSeq: 40}, calls[0].State)
}

func handedVoice(seq int) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         uint(seq),
		Src:         100,
		Dst:         200,
		GroupCall:   true,
		StreamID:    7,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA + uint(seq%6),
		BER:         -1,
		RSSI:        -1,
	}
}

// TestHandOverKeepsTheCallOnTime takes a peer in a call from a replica
// that hands over the bursts that it sent in the last hub.MoveSlack, as
// copies that the peer may have missed. Their time on the grid passed
// already, so the bursts of the call that come after them must go out on
// time and not wait behind them, or each move puts the rest of the call
// further behind.
func TestHandOverKeepsTheCallOnTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const id, copies, live = uint32(300860), 9, 10
		log := newSendLog()
		p := newPacer(clock.Real(), log.send, nil)
		t.Cleanup(p.stop)
		s := &IPSCServer{clock: clock.Real(), pacer: p, localID: 1, moving: map[uint32]*moving{id: newMoving()}}
		var report handoverReport
		for i := range copies {
			pkt := handedVoice(i)
			b, err := pkt.MarshalMsg(nil)
			require.NoError(t, err)
			report.Lost = append(report.Lost, b)
		}
		since := time.Now()
		s.finishHandover(id, since, &report)
		var last time.Time
		next := since
		for i := copies; i < copies+live; i++ {
			next = next.Add(burstTime)
			time.Sleep(time.Until(next))
			last = time.Now()
			s.route(hub.RoutedPacket{RepeaterID: uint(id), Packet: handedVoice(i)})
		}
	drain:
		for {
			select {
			case <-log.got:
			case <-time.After(copies * burstTime):
				break drain
			}
		}
		times := log.times(slotKey{peer: id})
		require.GreaterOrEqual(t, len(times), copies+live)
		assert.Less(t, times[len(times)-1].Sub(last), 2*burstTime, "the call fell behind the copies of the handover")
	})
}

func reportOf(t *testing.T, seqs ...int) *handoverReport {
	t.Helper()
	var r handoverReport
	for _, i := range seqs {
		pkt := handedVoice(i)
		b, err := pkt.MarshalMsg(nil)
		require.NoError(t, err)
		r.Lost = append(r.Lost, b)
	}
	return &r
}

// TestAnEarlyHandoverSendsWhatFollowsIt takes the early report of a replica
// that a peer moved from in a call, and then bursts that this replica gets
// for the peer: one that follows the report goes out at once, and the
// bursts after a gap wait for the full report, which brings the burst of
// the gap. The peer must get each burst once and in order.
func TestAnEarlyHandoverSendsWhatFollowsIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const id = uint32(300860)
		got := make(chan uint16, 20)
		p := newPacer(clock.Real(), func(_ slotKey, frame []byte) {
			u, err := ipscwire.DecodeUser(frame)
			if assert.NoError(t, err) {
				got <- u.RTP.Seq
			}
		}, nil)
		t.Cleanup(p.stop)
		s := &IPSCServer{clock: clock.Real(), pacer: p, localID: 1, moving: map[uint32]*moving{id: newMoving()}}
		since := time.Now()
		s.startHandover(id, since, reportOf(t, 0, 1, 2))
		s.route(hub.RoutedPacket{RepeaterID: uint(id), Packet: handedVoice(2)})
		s.route(hub.RoutedPacket{RepeaterID: uint(id), Packet: handedVoice(3)})
		var seqs []uint16
		next := func() {
			select {
			case seq := <-got:
				seqs = append(seqs, seq)
			case <-time.After(time.Second):
				require.FailNow(t, "a burst did not go out")
			}
		}
		for range 4 {
			next()
		}
		s.route(hub.RoutedPacket{RepeaterID: uint(id), Packet: handedVoice(5)})
		s.route(hub.RoutedPacket{RepeaterID: uint(id), Packet: handedVoice(6)})
		select {
		case seq := <-got:
			require.Failf(t, "a burst after a gap went out before the full report", "RTP sequence %d", seq)
		case <-time.After(3 * burstTime):
		}
		s.finishHandover(id, since, reportOf(t, 0, 1, 2, 3, 4))
		for range 3 {
			next()
		}
		select {
		case seq := <-got:
			require.Failf(t, "the peer got a burst again", "RTP sequence %d", seq)
		case <-time.After(3 * burstTime):
		}
		for i, seq := range seqs {
			assert.Equal(t, seqs[0]+uint16(i), seq, "burst %d", i) //nolint:gosec
		}
	})
}

// blockingPacer returns a pacer whose sends to a peer block until release
// gets a value, and the times at which the frames left.
func blockingPacer(t *testing.T) (p *pacer, left <-chan time.Time, release chan<- struct{}) {
	t.Helper()
	l := make(chan time.Time)
	r := make(chan struct{})
	p = newPacer(clock.Real(), func(slotKey, []byte) {
		l <- time.Now()
		<-r
	}, nil)
	t.Cleanup(p.stop)
	return p, l, r
}

// TestHandOverNoPacketThatLeftJustOverMoveSlackBeforeTheClaim sends a peer
// a packet whose send returns a moment after the packet left, just over
// MoveSlack before the packet of the peer reached the other replica. The
// load balancer moved the peer no earlier than that, so the peer got the
// packet before the move, and the handover must not hold it, or the peer
// gets it twice outside the move.
func TestHandOverNoPacketThatLeftJustOverMoveSlackBeforeTheClaim(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, left, release := blockingPacer(t)
		p.enqueue(slotKey{peer: 1}, 7, []byte{'v'}, false, handedVoice(3), &packetState{}, time.Time{})
		at := <-left
		time.Sleep(time.Millisecond)
		release <- struct{}{}
		synctest.Wait()
		_, lost, _ := p.handOver(1, at.Add(hub.MoveSlack+500*time.Microsecond))
		assert.Empty(t, lost, "the handover holds a packet that the peer got before the move")
	})
}

// TestHandOverAPacketWhoseSendStalledPastTheMove stalls the send of a
// packet to a peer for longer than MoveSlack, past the move of the peer,
// and builds the handover once the send returned. The packet may have left
// after the move, which the load balancer no longer takes to the peer, so
// the handover must hold it.
func TestHandOverAPacketWhoseSendStalledPastTheMove(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, left, release := blockingPacer(t)
		p.enqueue(slotKey{peer: 1}, 7, []byte{'v'}, false, handedVoice(3), &packetState{}, time.Time{})
		at := <-left
		time.Sleep(2 * hub.MoveSlack)
		release <- struct{}{}
		synctest.Wait()
		_, lost, _ := p.handOver(1, at.Add(hub.MoveSlack+hub.MoveSlack/2))
		assert.Len(t, lost, 1, "the handover misses a packet whose send ended after the move")
	})
}

// TestHandOverAPacketByWhenItsFirstFrameLeft sends a peer a voice header,
// which goes out as three frames a burst apart. A handover for a claim just
// over MoveSlack after the first frame left must not hold the packet, since
// the peer got that frame before the move and would get it again outside
// the move, and one for a claim just under MoveSlack after must hold it.
func TestHandOverAPacketByWhenItsFirstFrameLeft(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		after time.Duration
		held  int
	}{
		"just over MoveSlack":  {hub.MoveSlack + 500*time.Microsecond, 0},
		"just under MoveSlack": {hub.MoveSlack - 500*time.Microsecond, 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				p, left, release := blockingPacer(t)
				header := dataSync(7, enums.DataTypeVoiceLCHeader)
				for i := range 3 {
					var state *packetState
					if i == 2 {
						state = &packetState{}
					}
					p.enqueue(slotKey{peer: 1}, 7, []byte{'h'}, false, header, state, time.Time{})
				}
				first := <-left
				release <- struct{}{}
				for range 2 {
					<-left
					release <- struct{}{}
				}
				synctest.Wait()
				_, lost, _ := p.handOver(1, first.Add(c.after))
				assert.Len(t, lost, c.held)
			})
		})
	}
}
