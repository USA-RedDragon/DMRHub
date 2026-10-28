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
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hearVoiceCall(p *pacer, k slotKey, terminator bool) {
	const stream = 1
	p.heard(k, dataSync(stream, enums.DataTypeVoiceLCHeader))
	for range 6 {
		p.heard(k, voiceBurst(stream))
	}
	if terminator {
		p.heard(k, dataSync(stream, enums.DataTypeTerminatorWithLC))
	}
}

// TestPacerHoldsDataAfterAVoiceCallFromThePeer has a peer send a voice call
// to the hub, and then gives the hub a data call and a CSBK for that peer
// and slot from 0.1 to 2.0 s after the terminator of the peer. An XPR 8400
// that repeats a call from its own RF drops the data and CSBK that reach it
// in its call hang, so they must wait for callHangHold after the
// terminator of the peer.
func TestPacerHoldsDataAfterAVoiceCallFromThePeer(t *testing.T) {
	t.Parallel()
	for _, dt := range []enums.DataType{enums.DataTypeDataHeader, enums.DataTypeCSBK} {
		for _, after := range []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, time.Second, 1500 * time.Millisecond, 2 * time.Second} {
			t.Run(fmt.Sprintf("%v/%v", dt, after), func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					p, clock, log, _ := newHangPacer(t)
					k := slotKey{peer: 311860, slot: true}
					hearVoiceCall(p, k, true)
					end := clock.Now()
					clock.advance(after)
					p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, dt), nil, time.Time{})
					log.none(t, "a data or CSBK call went out in the call hang of the peer")
					clock.advance(callHangHold - after)
					got := log.next(t, time.Second)
					assert.Equal(t, byte('d'), got.tag)
					assert.GreaterOrEqual(t, got.at.Sub(end), callHangHold, "the held call went out before the hang of the peer ended")
				})
			})
		}
	}
}

// TestPacerSendsDataAfterTheHangOfThePeer gives the hub data for a peer
// once the call hang after a voice call of the peer is over. It must go
// out at once.
func TestPacerSendsDataAfterTheHangOfThePeer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, clock, log, _ := newHangPacer(t)
		k := slotKey{peer: 311860, slot: false}
		hearVoiceCall(p, k, true)
		clock.advance(callHangHold)
		p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeDataHeader), nil, time.Time{})
		assert.Equal(t, byte('d'), log.next(t, 200*time.Millisecond).tag)
	})
}

// TestPacerHoldsDataAfterAVoiceCallFromThePeerWithoutItsTerminator has a
// peer send a voice call whose terminator is lost. The data for the peer
// waits while the call goes on and for callHangHold after it times out,
// callHang after its last burst.
func TestPacerHoldsDataAfterAVoiceCallFromThePeerWithoutItsTerminator(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, clock, log, _ := newHangPacer(t)
		k := slotKey{peer: 311860, slot: true}
		hearVoiceCall(p, k, false)
		last := clock.Now()
		clock.advance(time.Second)
		p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeDataHeader), nil, time.Time{})
		log.none(t, "a data call went out in a voice call of the peer")
		clock.advance(callHang)
		log.none(t, "a data call went out in the call hang of the peer")
		clock.advance(callHangHold - time.Second)
		got := log.next(t, time.Second)
		assert.Equal(t, byte('d'), got.tag)
		assert.GreaterOrEqual(t, got.at.Sub(last), callHang+callHangHold)
	})
}

// TestPacerHoldsDataAfterADataCallFromThePeer has a peer send an SMS to the
// hub. The data for the peer waits dataHangHold after the last block.
func TestPacerHoldsDataAfterADataCallFromThePeer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, clock, log, _ := newHangPacer(t)
		k := slotKey{peer: smsPeer, slot: false}
		for _, raw := range smsBursts(t, "hang") {
			p.heard(k, smsPacketOf(1, raw))
		}
		end := clock.Now()
		clock.advance(500 * time.Millisecond)
		p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeDataHeader), nil, time.Time{})
		log.none(t, "a data call went out in the data hang of the peer")
		clock.advance(dataHangHold - 500*time.Millisecond)
		got := log.next(t, time.Second)
		assert.Equal(t, byte('d'), got.tag)
		assert.GreaterOrEqual(t, got.at.Sub(end), dataHangHold)
	})
}

// TestPacerHangOfThePeerHoldsOnlyDataOnItsSlot has a peer send a voice call
// on one slot. Voice to that slot, and data to the other slot and to
// another peer, go out at once.
func TestPacerHangOfThePeerHoldsOnlyDataOnItsSlot(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, _, log, _ := newHangPacer(t)
		held := slotKey{peer: 311860, slot: false}
		hearVoiceCall(p, held, true)
		p.enqueue(held, 2, []byte{'v'}, false, voiceBurst(2), nil, time.Time{})
		assert.Equal(t, held, log.next(t, 100*time.Millisecond).key)
		for i, k := range []slotKey{{peer: 311860, slot: true}, {peer: 311861, slot: false}} {
			stream := uint(10 + i)
			p.enqueue(k, stream, []byte{'o'}, false, dataSync(stream, enums.DataTypeDataHeader), nil, time.Time{})
			assert.Equal(t, k, log.next(t, 100*time.Millisecond).key)
		}
	})
}

// TestHandOverCarriesTheCallHangOfThePeer moves a peer to another replica
// right after a voice call from it. The handover carries the rest of the
// hang.
func TestHandOverCarriesTheCallHangOfThePeer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, clock, _, _ := newHangPacer(t)
		k := slotKey{peer: 311860, slot: true}
		hearVoiceCall(p, k, true)
		clock.advance(time.Second)
		_, _, hangs := p.handOver(k.peer, time.Now())
		require.Len(t, hangs, 1)
		assert.Equal(t, k.slot, hangs[0].Slot)
		assert.InDelta(t, float64(callHangHold-time.Second), float64(hangs[0].Left), float64(100*time.Millisecond))
	})
}

// TestServerHoldsDataAfterAnXPRVoiceCall replays a voice call of an XPR 8400
// into the server, and then gives the server data for that repeater and
// slot.
func TestServerHoldsDataAfterAnXPRVoiceCall(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, clock, log, _ := newHangPacer(t)
		s := &IPSCServer{clock: clock, pacer: p, localID: 1, translator: NewIPSCTranslator(1)}
		call := readXPRCapture(t, "voicecall")
		for _, raw := range call {
			s.fromPeer(t.Context(), raw[0], raw, time.Now())
		}
		u := decodeUserT(t, call[0])
		k := slotKey{peer: u.ID, slot: u.Info.Slot() == enums.Slot2}
		p.enqueue(k, 99, []byte{'d'}, false, dataSync(99, enums.DataTypeDataHeader), nil, time.Time{})
		log.none(t, "data went out in the call hang of the repeater")
		clock.advance(callHangHold)
		assert.Equal(t, byte('d'), log.next(t, time.Second).tag)
	})
}

// TestPacerHoldsDataAfterADataCallFromThePeerWithoutItsLastBlock has a peer
// send an SMS whose last block is lost. The data for the peer waits for
// dataHangHold after the call times out, callHang after its last block.
func TestPacerHoldsDataAfterADataCallFromThePeerWithoutItsLastBlock(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p, clock, log, _ := newHangPacer(t)
		k := slotKey{peer: smsPeer, slot: false}
		bursts := smsBursts(t, "hang")
		require.Greater(t, len(bursts), 2)
		for _, raw := range bursts[:len(bursts)-1] {
			p.heard(k, smsPacketOf(1, raw))
		}
		last := clock.Now()
		p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeDataHeader), nil, time.Time{})
		log.none(t, "a data call went out in a data call of the peer")
		clock.advance(callHang)
		log.none(t, "a data call went out in the data hang of the peer")
		clock.advance(dataHangHold)
		got := log.next(t, time.Second)
		assert.Equal(t, byte('d'), got.tag)
		assert.GreaterOrEqual(t, got.at.Sub(last), callHang+dataHangHold)
	})
}
