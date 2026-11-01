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
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// browserBurst is a burst of a call to a browser hotspot.
type browserBurst struct {
	slot2  bool
	stream uint
	seq    uint
}

// sendBrowserBursts sends bursts to the browser hotspot 1 of s, with a
// pause of 60 ms after each, and returns the time at which the burst at
// moveAt left.
func sendBrowserBursts(s *Server, bursts []browserBurst, moveAt int, pause func(time.Duration)) time.Time {
	var at time.Time
	for i, b := range bursts {
		if i == moveAt {
			at = time.Now()
		}
		s.deliver(context.Background(), 1, models.Packet{Slot: b.slot2, StreamID: b.stream, Seq: b.seq})
		s.writeOut(<-s.outgoingChan)
		pause(60 * time.Millisecond)
	}
	return at
}

// browserServer is a queueServer whose repeater 1 is a browser hotspot.
func browserServer() *Server {
	s := queueServer()
	s.connected.Store(1, endpoint{transport: webTransportName, addr: wtAddr("a")})
	return s
}

func oneCall(n int) []browserBurst {
	out := make([]browserBurst, 0, n)
	for i := range n {
		out = append(out, browserBurst{stream: 7, seq: uint(i)})
	}
	return out
}

func burstsOf(pkts []models.Packet) []browserBurst {
	out := make([]browserBurst, 0, len(pkts))
	for _, p := range pkts {
		out = append(out, browserBurst{slot2: p.Slot, stream: p.StreamID, seq: p.Seq})
	}
	return out
}

// handedOver hands the browser hotspot 1 of s over from since, as the
// replica that it leaves does, and returns what the replica that takes it
// over with anchors sends it.
func handedOver(t *testing.T, s *Server, since time.Time, anchors []wtAnchor) []browserBurst {
	t.Helper()
	b, err := json.Marshal(s.report(1, since, handoverRule{slack: hub.MoveSlack}))
	require.NoError(t, err)
	var h handover
	require.NoError(t, json.Unmarshal(b, &h))
	return burstsOf(h.lost(anchors))
}

// TestAHandoverWithoutAnchorsHoldsTheTimeWindow hands a browser hotspot
// that sent no anchors over, as an older browser does. It must get what
// left in hub.MoveSlack before the move and after it, as before anchors.
func TestAHandoverWithoutAnchorsHoldsTheTimeWindow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := browserServer()
		since := sendBrowserBursts(s, oneCall(20), 15, time.Sleep)
		assert.Equal(t, oneCall(20)[7:], handedOver(t, s, since, nil))
	})
}

// TestAHandoverStartsAfterTheAnchor hands over a browser hotspot whose
// anchor is a burst that left long before the time window of the move, as
// when a load balancer that stalls lost the bursts after it. The hotspot
// must get every burst after its anchor, in order, and none before.
func TestAHandoverStartsAfterTheAnchor(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := browserServer()
		since := sendBrowserBursts(s, oneCall(20), 15, time.Sleep)
		assert.Equal(t, oneCall(20)[3:], handedOver(t, s, since, []wtAnchor{{Slot: 1, Stream: 7, Seq: 2}}))
	})
}

// TestAHandoverToABrowserThatTookNothing hands over a browser hotspot
// that sent anchors for no slot: it took no burst. It must get all that
// the replica kept.
func TestAHandoverToABrowserThatTookNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := browserServer()
		since := sendBrowserBursts(s, oneCall(20), 15, time.Sleep)
		assert.Equal(t, oneCall(20), handedOver(t, s, since, []wtAnchor{}))
	})
}

// TestAHandoverFromAnAnchorOlderThanTheHistory hands over a browser
// hotspot whose anchor left before the oldest burst that the replica
// keeps. The hotspot took none of the bursts that the replica keeps, so it
// must get all of them.
func TestAHandoverFromAnAnchorOlderThanTheHistory(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := browserServer()
		calls := oneCall(100)
		since := sendBrowserBursts(s, calls, 95, time.Sleep)
		got := handedOver(t, s, since, []wtAnchor{{Slot: 1, Stream: 7, Seq: 5}})
		assert.Equal(t, calls[100-sentKeep:], got)
	})
}

// TestAHandoverStartsAfterTheAnchorOfEachSlot hands over a browser hotspot
// with a call on each slot, which took more of one than of the other. It
// must get the bursts of each slot after the anchor of that slot, in the
// order they left.
func TestAHandoverStartsAfterTheAnchorOfEachSlot(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := browserServer()
		calls := make([]browserBurst, 0, 20)
		for i := range 10 {
			calls = append(calls, browserBurst{stream: 7, seq: uint(i)}, browserBurst{slot2: true, stream: 8, seq: uint(i)})
		}
		since := sendBrowserBursts(s, calls, 19, time.Sleep)
		got := handedOver(t, s, since, []wtAnchor{{Slot: 1, Stream: 7, Seq: 2}, {Slot: 2, Stream: 8, Seq: 5}})
		var want []browserBurst
		for _, b := range calls {
			if (!b.slot2 && b.seq > 2) || (b.slot2 && b.seq > 5) {
				want = append(want, b)
			}
		}
		assert.Equal(t, want, got)
	})
}

// TestAnAnchoredHandoverFromAnOlderReplica hands a browser hotspot with an
// anchor over from a replica that sends only the time window, as one
// before anchors does. The hotspot must get the bursts of the window after
// its anchor.
func TestAnAnchoredHandoverFromAnOlderReplica(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := browserServer()
		since := sendBrowserBursts(s, oneCall(20), 15, time.Sleep)
		b, err := json.Marshal(map[string][][]byte{"packets": encodePackets(s.sentSince(since, handoverRule{slack: hub.MoveSlack}))})
		require.NoError(t, err)
		var old handover
		require.NoError(t, json.Unmarshal(b, &old))
		assert.Equal(t, oneCall(20)[11:], burstsOf(old.lost([]wtAnchor{{Slot: 1, Stream: 7, Seq: 10}})))
	})
}

// TestAPathMoveSendsAgainWhatCameAfterTheAnchor moves the QUIC path of a
// browser hotspot that answers the anchor question, after the load
// balancer lost more than hub.MoveSlack of bursts. The replica must send
// again, in order, each burst after the anchor, and none before.
func TestAPathMoveSendsAgainWhatCameAfterTheAnchor(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := browserServer()
		s.outgoingChan = make(chan outgoing, 64)
		var asked []wtAddr
		s.anchorsOf = func(a wtAddr) ([]wtAnchor, bool) {
			asked = append(asked, a)
			return []wtAnchor{{Slot: 1, Stream: 7, Seq: 2}}, true
		}
		sendBrowserBursts(s, oneCall(30), 0, time.Sleep)
		s.resendMoved(wtAddr("a"), time.Now())
		synctest.Wait()
		var got []browserBurst
		for len(s.outgoingChan) > 0 {
			p, ok := models.UnpackPacket((<-s.outgoingChan).raw.Data)
			require.True(t, ok)
			got = append(got, browserBurst{slot2: p.Slot, stream: p.StreamID, seq: p.Seq})
			time.Sleep(wtHandoverGap)
			synctest.Wait()
		}
		assert.Equal(t, []wtAddr{"a"}, asked)
		assert.Equal(t, oneCall(30)[3:], got)
	})
}

// TestAPathMoveWithoutAnAnswerSendsAgainTheTimeWindow moves the QUIC path
// of a browser hotspot that does not answer the anchor question, as an
// older browser. The replica must send again what left in hub.MoveSlack
// before the move, as before anchors.
func TestAPathMoveWithoutAnAnswerSendsAgainTheTimeWindow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := browserServer()
		s.outgoingChan = make(chan outgoing, 64)
		s.anchorsOf = func(wtAddr) ([]wtAnchor, bool) { return nil, false }
		sendBrowserBursts(s, oneCall(30), 0, time.Sleep)
		s.resendMoved(wtAddr("a"), time.Now())
		synctest.Wait()
		var got []browserBurst
		for len(s.outgoingChan) > 0 {
			p, ok := models.UnpackPacket((<-s.outgoingChan).raw.Data)
			require.True(t, ok)
			got = append(got, browserBurst{slot2: p.Slot, stream: p.StreamID, seq: p.Seq})
			time.Sleep(wtHandoverGap)
			synctest.Wait()
		}
		assert.Equal(t, oneCall(30)[22:], got)
	})
}
