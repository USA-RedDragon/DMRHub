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
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	mu      sync.Mutex
	offset  time.Duration
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

func (c *fakeClock) after(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.mu.Lock()
	c.waiters = append(c.waiters, fakeWaiter{at: time.Now().Add(c.offset + d), ch: ch})
	c.mu.Unlock()
	time.AfterFunc(d, c.fire)
	return ch
}

func (c *fakeClock) endHang() {
	c.mu.Lock()
	c.offset += callHangHold
	c.mu.Unlock()
	c.fire()
}

func (c *fakeClock) fire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().Add(c.offset)
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if now.Before(w.at) {
			kept = append(kept, w)
			continue
		}
		w.ch <- now
	}
	c.waiters = kept
}

type hangSend struct {
	key slotKey
	tag byte
	at  time.Time
}

type hangLog struct {
	clock *fakeClock
	mu    sync.Mutex
	sent  []hangSend
	got   chan hangSend
}

func newHangPacer(t *testing.T) (*pacer, *fakeClock, *hangLog, *[]uint) {
	t.Helper()
	clock := &fakeClock{}
	log := &hangLog{clock: clock, got: make(chan hangSend, 1000)}
	var mu sync.Mutex
	var discarded []uint
	p := newPacer(func(k slotKey, frame []byte) {
		s := hangSend{key: k, tag: frame[0], at: clock.now()}
		log.mu.Lock()
		log.sent = append(log.sent, s)
		log.mu.Unlock()
		log.got <- s
	}, func(pkt models.Packet) {
		mu.Lock()
		discarded = append(discarded, pkt.StreamID)
		mu.Unlock()
	})
	p.now = clock.now
	p.after = clock.after
	t.Cleanup(p.stop)
	return p, clock, log, &discarded
}

func (l *hangLog) next(t *testing.T, within time.Duration) hangSend {
	t.Helper()
	select {
	case s := <-l.got:
		return s
	case <-time.After(within):
		t.Fatalf("nothing was sent within %v", within)
		return hangSend{}
	}
}

func (l *hangLog) none(t *testing.T, why string) {
	t.Helper()
	select {
	case s := <-l.got:
		t.Fatalf("%c was sent: %s", s.tag, why)
	case <-time.After(300 * time.Millisecond):
	}
}

func (l *hangLog) tags() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := make([]byte, 0, len(l.sent))
	for _, s := range l.sent {
		b = append(b, s.tag)
	}
	return string(b)
}

func dataSync(stream uint, dt enums.DataType) models.Packet {
	return models.Packet{StreamID: stream, FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(dt)}
}

func voiceBurst(stream uint) models.Packet {
	return models.Packet{StreamID: stream, FrameType: dmrconst.FrameVoice}
}

func sendVoiceCall(p *pacer, k slotKey, stream uint, tag byte) {
	p.enqueue(k, stream, []byte{tag}, false, dataSync(stream, enums.DataTypeVoiceLCHeader), nil)
	p.enqueue(k, stream, []byte{tag}, false, voiceBurst(stream), nil)
	p.enqueue(k, stream, []byte{tag - 'a' + 'A'}, true, dataSync(stream, enums.DataTypeTerminatorWithLC), nil)
}

// TestPacerHoldsDataAndCSBKInTheCallHang sends a data call and a CSBK to a
// peer right after a voice call on the slot. A repeater drops the data and
// CSBK calls that reach it during its call hang, so they must wait for
// callHangHold after the terminator went out.
func TestPacerHoldsDataAndCSBKInTheCallHang(t *testing.T) {
	t.Parallel()
	for name, dt := range map[string]enums.DataType{"data": enums.DataTypeDataHeader, "csbk": enums.DataTypeCSBK} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, clock, log, _ := newHangPacer(t)
			k := slotKey{peer: 311860, slot: true}
			sendVoiceCall(p, k, 1, 'v')
			var end hangSend
			for range 3 {
				end = log.next(t, time.Second)
			}
			require.Equal(t, byte('V'), end.tag)

			p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, dt), nil)
			log.none(t, "a data or CSBK call went out in the call hang")
			clock.endHang()
			got := log.next(t, time.Second)
			assert.Equal(t, byte('d'), got.tag)
			assert.GreaterOrEqual(t, got.at.Sub(end.at), callHangHold, "the held call went out before the hang ended")
		})
	}
}

// TestPacerHoldsDataAfterADataCall sends a data call of 20 blocks to a peer
// at once, and then another data call. The pacer sends the blocks over more
// than a second, and the second call must wait for dataHangHold after the
// last block went out, not after the last block came.
func TestPacerHoldsDataAfterADataCall(t *testing.T) {
	t.Parallel()
	p, _, log, _ := newHangPacer(t)
	k := slotKey{peer: 311860, slot: false}
	p.enqueue(k, 1, []byte{'h'}, false, dataSync(1, enums.DataTypeDataHeader), nil)
	for range 19 {
		p.enqueue(k, 1, []byte{'b'}, false, dataSync(1, enums.DataTypeRate12), nil)
	}
	p.enqueue(k, 2, []byte{'e'}, false, dataSync(2, enums.DataTypeDataHeader), nil)
	var last hangSend
	for range 20 {
		last = log.next(t, time.Second)
	}
	got := log.next(t, callHang+dataHangHold+time.Second)
	require.Equal(t, byte('e'), got.tag)
	assert.GreaterOrEqual(t, got.at.Sub(last.at), dataHangHold, "a data call went out too soon after the last one")
}

// TestPacerNeverHoldsVoice sends a voice call to a peer while a data call
// waits for the call hang of an earlier voice call. The voice goes out at
// once, and the data call then waits for the hang after it.
func TestPacerNeverHoldsVoice(t *testing.T) {
	t.Parallel()
	p, clock, log, _ := newHangPacer(t)
	k := slotKey{peer: 311860, slot: false}
	sendVoiceCall(p, k, 1, 'v')
	for range 3 {
		log.next(t, time.Second)
	}
	p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeDataHeader), nil)
	sendVoiceCall(p, k, 3, 'w')
	var end hangSend
	for range 3 {
		end = log.next(t, 500*time.Millisecond)
	}
	require.Equal(t, byte('W'), end.tag)
	log.none(t, "the data call went out in the hang of the second voice call")
	clock.endHang()
	got := log.next(t, time.Second)
	assert.GreaterOrEqual(t, got.at.Sub(end.at), callHangHold)
	assert.Equal(t, "vvVwwWd", log.tags())
}

// TestPacerHoldNeverHoldsAnotherSlotOrPeer sends data to the other slot of
// a peer and to another peer while a slot is in its call hang.
func TestPacerHoldNeverHoldsAnotherSlotOrPeer(t *testing.T) {
	t.Parallel()
	p, _, log, _ := newHangPacer(t)
	held := slotKey{peer: 311860, slot: false}
	sendVoiceCall(p, held, 1, 'v')
	for range 3 {
		log.next(t, time.Second)
	}
	p.enqueue(held, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeDataHeader), nil)
	for i, k := range []slotKey{{peer: 311860, slot: true}, {peer: 311861, slot: false}} {
		stream := uint(10 + i)
		p.enqueue(k, stream, []byte{'o'}, false, dataSync(stream, enums.DataTypeDataHeader), nil)
		got := log.next(t, 100*time.Millisecond)
		assert.Equal(t, k, got.key)
	}
}

// TestPacerBoundsTheWaitingCalls holds more calls on a slot than it keeps.
// The call past the bound is discarded as a whole and reported, with the
// bursts of it that come later, and a waiting call that grows past
// maxWaitingFrames is discarded as a whole too.
func TestPacerBoundsTheWaitingCalls(t *testing.T) {
	t.Parallel()
	p, clock, log, discarded := newHangPacer(t)
	k := slotKey{peer: 311860, slot: false}
	sendVoiceCall(p, k, 1, 'v')
	for range 3 {
		log.next(t, time.Second)
	}
	for i := range maxWaitingCalls + 1 {
		stream := uint(100 + i)
		p.enqueue(k, stream, []byte{'d'}, false, dataSync(stream, enums.DataTypeDataHeader), nil)
	}
	over := uint(100 + maxWaitingCalls)
	p.enqueue(k, over, []byte{'d'}, false, dataSync(over, enums.DataTypeRate12), nil)
	big := uint(100)
	for range maxWaitingFrames {
		p.enqueue(k, big, []byte{'b'}, false, dataSync(big, enums.DataTypeRate12), nil)
	}
	want := make([]uint, 0, 2+maxWaitingFrames+1)
	want = append(want, over, over)
	for range maxWaitingFrames + 1 {
		want = append(want, big)
	}
	assert.Equal(t, want, *discarded)
	assert.Equal(t, maxWaitingCalls-1, p.queued(), "the frames of the calls that still wait")

	clock.endHang()
	got := log.next(t, time.Second)
	assert.Equal(t, byte('d'), got.tag)
}

// TestHandOverCarriesTheCallHang moves a peer to another replica right
// after a voice call to it. The other replica must hold the data to the
// peer for the rest of the hang.
func TestHandOverCarriesTheCallHang(t *testing.T) {
	t.Parallel()
	from, _, fromLog, _ := newHangPacer(t)
	k := slotKey{peer: 311860, slot: true}
	sendVoiceCall(from, k, 1, 'v')
	for range 3 {
		fromLog.next(t, time.Second)
	}
	_, _, hangs := from.handOver(k.peer, time.Now())
	require.Len(t, hangs, 1)
	assert.Equal(t, k.slot, hangs[0].Slot)
	assert.Greater(t, hangs[0].Left, callHangHold-time.Second)

	to, clock, toLog, _ := newHangPacer(t)
	for _, h := range hangs {
		to.holdFor(slotKey{peer: k.peer, slot: h.Slot}, h.Left)
	}
	to.enqueue(k, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeDataHeader), nil)
	toLog.none(t, "the other replica sent data in the call hang")
	clock.endHang()
	assert.Equal(t, byte('d'), toLog.next(t, time.Second).tag)
}

// TestFinishHandoverHoldsForTheReportedHang takes the report of a replica
// that sent a voice call to the peer just before the peer moved here.
func TestFinishHandoverHoldsForTheReportedHang(t *testing.T) {
	t.Parallel()
	p, clock, log, _ := newHangPacer(t)
	s := &IPSCServer{pacer: p, localID: 1}
	b, err := json.Marshal(handoverReport{Hangs: []handedHang{{Slot: true, Left: callHangHold}}})
	require.NoError(t, err)
	var r handoverReport
	require.NoError(t, json.Unmarshal(b, &r))
	s.finishHandover(311860, &r)

	k := slotKey{peer: 311860, slot: true}
	p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeCSBK), nil)
	log.none(t, "a CSBK went out in the reported call hang")
	clock.endHang()
	assert.Equal(t, byte('d'), log.next(t, time.Second).tag)
}
