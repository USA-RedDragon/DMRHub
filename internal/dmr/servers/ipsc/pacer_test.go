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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sendLog struct {
	mu   sync.Mutex
	sent map[slotKey][]time.Time
	got  chan slotKey
}

func newSendLog() *sendLog {
	return &sendLog{sent: map[slotKey][]time.Time{}, got: make(chan slotKey, 1000)}
}

func (l *sendLog) send(k slotKey, _ []byte) {
	l.mu.Lock()
	l.sent[k] = append(l.sent[k], time.Now())
	l.mu.Unlock()
	l.got <- k
}

func (l *sendLog) times(k slotKey) []time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]time.Time(nil), l.sent[k]...)
}

func TestPacerSpacesASlotAtTheBurstTime(t *testing.T) {
	t.Parallel()
	log := newSendLog()
	p := newPacer(log.send, nil)
	defer p.stop()

	k := slotKey{peer: 311860, slot: false}
	start := time.Now()
	for range 6 {
		p.enqueue(k, 1, []byte{1}, false, models.Packet{}, nil)
	}
	for range 6 {
		<-log.got
	}
	times := log.times(k)
	require.Len(t, times, 6)
	assert.Less(t, times[0].Sub(start), 30*time.Millisecond, "the first burst on an idle slot goes at once")
	for i := 1; i < len(times); i++ {
		assert.GreaterOrEqual(t, times[i].Sub(times[i-1]), burstTime-2*time.Millisecond, "gap before burst %d", i)
	}
}

func TestPacerNeverHoldsAnotherSlotOrPeer(t *testing.T) {
	t.Parallel()
	log := newSendLog()
	p := newPacer(log.send, nil)
	defer p.stop()

	busy := slotKey{peer: 311860, slot: false}
	for range 20 {
		p.enqueue(busy, 1, []byte{1}, false, models.Packet{}, nil)
	}
	<-log.got
	for _, k := range []slotKey{{peer: 311860, slot: true}, {peer: 311861, slot: false}} {
		start := time.Now()
		p.enqueue(k, 2, []byte{2}, false, models.Packet{}, nil)
		for got := range log.got {
			if got == k {
				break
			}
		}
		assert.Less(t, time.Since(start), 30*time.Millisecond, "%+v waited behind a busy slot", k)
	}
}

func TestPacerDoesNotFallBehindALiveStream(t *testing.T) {
	t.Parallel()
	log := newSendLog()
	p := newPacer(log.send, nil)
	defer p.stop()

	k := slotKey{peer: 311860, slot: true}
	var last time.Time
	for range 20 {
		time.Sleep(burstTime)
		at := time.Now()
		p.enqueue(k, 1, []byte{1}, false, models.Packet{}, nil)
		<-log.got
		last = at
	}
	times := log.times(k)
	assert.Less(t, times[len(times)-1].Sub(last), 30*time.Millisecond, "a stream at the burst time is sent as it arrives")
}

func TestPacerQueuesEveryBurst(t *testing.T) {
	t.Parallel()
	var started atomic.Int64
	release := make(chan struct{})
	p := newPacer(func(slotKey, []byte) {
		started.Add(1)
		<-release
	}, nil)
	defer p.stop()
	defer close(release)

	k := slotKey{peer: 311860, slot: false}
	const n = 5000
	for range n {
		p.enqueue(k, 1, []byte{1}, false, models.Packet{}, nil)
	}
	require.Eventually(t, func() bool { return started.Load() == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, n-1, p.queued(), "bursts waiting behind the one being sent")
}

type frameLog struct {
	mu   sync.Mutex
	sent []byte
	got  chan struct{}
}

func (l *frameLog) send(_ slotKey, frame []byte) {
	l.mu.Lock()
	l.sent = append(l.sent, frame[0])
	l.mu.Unlock()
	l.got <- struct{}{}
}

func TestPacerQueuesASecondCallBehindTheFirst(t *testing.T) {
	t.Parallel()
	log := &frameLog{got: make(chan struct{}, 100)}
	p := newPacer(log.send, nil)
	defer p.stop()

	k := slotKey{peer: 311860, slot: false}
	p.enqueue(k, 1, []byte{'a'}, false, models.Packet{}, nil)
	p.enqueue(k, 2, []byte{'B'}, false, models.Packet{}, nil)
	p.enqueue(k, 1, []byte{'a'}, false, models.Packet{}, nil)
	p.enqueue(k, 2, []byte{'B'}, true, models.Packet{}, nil)
	p.enqueue(k, 1, []byte{'a'}, true, models.Packet{}, nil)
	for range 5 {
		<-log.got
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	assert.Equal(t, "aaaBB", string(log.sent))
}

func TestPacerStartsTheNextCallAfterALostTerminator(t *testing.T) {
	t.Parallel()
	log := &frameLog{got: make(chan struct{}, 100)}
	p := newPacer(log.send, nil)
	defer p.stop()

	k := slotKey{peer: 311860, slot: true}
	p.enqueue(k, 1, []byte{'a'}, false, models.Packet{}, nil)
	<-log.got
	start := time.Now()
	p.enqueue(k, 2, []byte{'B'}, true, models.Packet{}, nil)
	select {
	case <-log.got:
	case <-time.After(callHang + time.Second):
		t.Fatal("the waiting call never started")
	}
	assert.GreaterOrEqual(t, time.Since(start), callHang-100*time.Millisecond, "the waiting call started before the active one went quiet")
}

func TestPacerDiscardsACallThatWaitedTooLong(t *testing.T) {
	t.Parallel()
	log := &frameLog{got: make(chan struct{}, 100)}
	var mu sync.Mutex
	var discarded []uint
	p := newPacer(log.send, func(pkt models.Packet) {
		mu.Lock()
		discarded = append(discarded, pkt.StreamID)
		mu.Unlock()
	})
	defer p.stop()
	var offset atomic.Int64
	p.now = func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }

	k := slotKey{peer: 311860, slot: false}
	p.enqueue(k, 1, []byte{'a'}, false, models.Packet{StreamID: 1}, nil)
	<-log.got
	p.enqueue(k, 2, []byte{'B'}, false, models.Packet{StreamID: 2}, nil)
	p.enqueue(k, 2, []byte{'B'}, true, models.Packet{StreamID: 2}, nil)
	offset.Store(int64(maxCallWait + time.Second))
	p.enqueue(k, 1, []byte{'a'}, true, models.Packet{StreamID: 1}, nil)
	p.enqueue(k, 2, []byte{'B'}, false, models.Packet{StreamID: 2}, nil)
	<-log.got
	select {
	case <-log.got:
		log.mu.Lock()
		t.Fatalf("a discarded call was sent: %q", log.sent)
	case <-time.After(300 * time.Millisecond):
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []uint{2, 2, 2}, discarded, "every burst of the discarded call, later ones too")
}
