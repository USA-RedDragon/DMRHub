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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/quic-go/quic-go"
)

const (
	// anchorParam is the query parameter of the URL of a session whose
	// browser answers the anchor question, with the version that it
	// answers, anchorVersion.
	anchorParam   = "anchor"
	anchorVersion = "1"
	// maxAckDelay is the max_ack_delay of QUIC that the browsers keep
	// (RFC 9000, section 18.2).
	maxAckDelay = 25 * time.Millisecond
)

// quicConnKey is the key of the QUIC connection of a request in its
// context.
type quicConnKey struct{}

// wtAsk is the question that a replica asks a browser hotspot on a stream
// that it opens: Ask "anchor" asks for its anchors.
type wtAsk struct {
	Ask string `json:"ask"`
}

// wtAnchors is the answer of a browser hotspot to an anchor question.
type wtAnchors struct {
	Anchor []wtAnchor `json:"anchor"`
}

// wtAnchor is the anchor of a browser hotspot on one slot: the last burst
// of the slot that it took with no burst of its stream missing before it,
// and First, the first burst of that stream that it took. A hotspot that
// knows what it got sends its anchors to the replica that takes it over,
// and to the replica whose path to it moved, which then sends it every
// burst that went to it after them, and the bursts of the stream before
// First, which it lost, rather than those of a time window, which a load
// balancer that stalls can outlast. An older hotspot sends no First, so
// it gets the time window as well, as it may have lost the first bursts
// of the stream of its anchor.
type wtAnchor struct {
	Slot   uint  `json:"slot"`
	Stream uint  `json:"stream"`
	Seq    uint  `json:"seq"`
	First  *uint `json:"first,omitempty"`
}

// slot2 reports whether the anchor is on slot 2.
func (a wtAnchor) slot2() bool {
	return a.Slot == 2
}

// afterAnchors returns the packets of sent, which went to a browser
// hotspot in that order, that the hotspot lost by its anchors, in order.
// Of a slot whose anchor is in sent, these are the packets after its last
// copy there and the packets of its stream before the copy of First that
// precedes it; without First, they are those after the anchor and those
// from window on, the packets of the time window of the move, of which
// the hotspot drops a copy of one that it took. Of a slot with no anchor,
// or whose anchor is older than sent, they are all of them, as the hotspot
// took none of them.
func afterAnchors(sent []models.Packet, anchors []wtAnchor, window int) []models.Packet {
	type kept struct {
		from   int
		stream uint
		before int
	}
	slots := map[bool]kept{}
	for _, a := range anchors {
		if a.Slot != 1 && a.Slot != 2 {
			continue
		}
		at := lastCopy(sent, a.slot2(), a.Stream, a.Seq, len(sent)-1)
		if at < 0 {
			continue
		}
		if a.First == nil {
			slots[a.slot2()] = kept{from: min(at+1, window)}
			continue
		}
		slots[a.slot2()] = kept{from: at + 1, stream: a.Stream, before: max(lastCopy(sent, a.slot2(), a.Stream, *a.First, at), 0)}
	}
	out := make([]models.Packet, 0, len(sent))
	for i, p := range sent {
		k := slots[p.Slot]
		if i >= k.from || (i < k.before && p.StreamID == k.stream) {
			out = append(out, p)
		}
	}
	return out
}

// lastCopy returns the index of the last packet of sent up to index to
// that is the burst seq of stream on slot2, or -1.
func lastCopy(sent []models.Packet, slot2 bool, stream, seq uint, to int) int {
	for i := to; i >= 0; i-- {
		p := sent[i]
		if p.Slot == slot2 && p.StreamID == stream && p.Seq&0xff == seq&0xff {
			return i
		}
	}
	return -1
}

// anchorWait returns how long a replica waits for the anchors of a browser
// on the QUIC connection c: three probe timeouts of the connection (RFC
// 9002, section 6.2.1), so that a lost packet of the question or the
// answer is sent again in time, but at least hub.MoveSlack, the time
// window that the replica sends again without anchors, and at most
// controlWait.
func anchorWait(c *quic.Conn) time.Duration {
	st := c.ConnectionStats()
	pto := st.SmoothedRTT + max(4*st.MeanDeviation, time.Millisecond) + maxAckDelay
	return min(controlWait, max(hub.MoveSlack, 3*pto))
}

// askAnchors asks the browser hotspot of the session at addr for its
// anchors, and reports false when its URL did not say that it answers, or
// it did not answer in anchorWait.
func (t *webTransport) askAnchors(addr wtAddr) ([]wtAnchor, bool) {
	c, ok := t.anchored.Load(string(addr))
	if !ok {
		return nil, false
	}
	sess, ok := t.sessions.Load(string(addr))
	if !ok {
		return nil, false
	}
	wait := anchorWait(c)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	str, err := sess.OpenStreamSync(ctx)
	if err != nil {
		return nil, false
	}
	defer str.CancelRead(0)
	_ = str.SetDeadline(time.Now().Add(wait))
	b, err := json.Marshal(wtAsk{Ask: anchorParam})
	if err != nil {
		return nil, false
	}
	if _, err := str.Write(append(b, '\n')); err != nil {
		return nil, false
	}
	_ = str.Close()
	line, err := bufio.NewReader(io.LimitReader(str, maxControl)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false
	}
	var a wtAnchors
	if json.Unmarshal(line, &a) != nil || a.Anchor == nil {
		return nil, false
	}
	return a.Anchor, true
}
