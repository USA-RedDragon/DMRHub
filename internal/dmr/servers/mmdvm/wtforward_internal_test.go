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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWTConnNotesAPathMove sends the packets of a connection of a replica
// first straight to it, then through another replica, then through a
// third, and then straight again. The replica must note each change of the
// way, and only those, so that it can send again what it sent on the old
// way, which the load balancer drops.
func TestWTConnNotesAPathMove(t *testing.T) {
	t.Parallel()
	c := &wtConn{tag: replicaTag{1, 2, 3, 4}, origins: map[string]origin{}, paths: map[string]path{}}
	pkt := make([]byte, 1+cidLen+8)
	pkt[0] = 0x40
	copy(pkt[1:], c.tag[:])
	browser := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1000}
	viaB := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2000}
	viaC := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3000}
	b, cc := replicaTag{5, 6, 7, 8}, replicaTag{9, 10, 11, 12}

	_, _, back := c.foreign(pkt, browser)
	assert.False(t, back, "a packet on the way it came was noted as a move")
	known, moved := c.pathFrom(b, viaB, pkt)
	assert.True(t, moved, "the move to another replica was not noted")
	assert.Equal(t, browser.String(), known.String())
	_, moved = c.pathFrom(b, viaB, pkt)
	assert.False(t, moved, "a packet on the way it came was noted as a move")
	_, moved = c.pathFrom(cc, viaC, pkt)
	assert.True(t, moved, "the move to a third replica was not noted")
	_, _, back = c.foreign(pkt, browser)
	assert.True(t, back, "the move back was not noted")
	_, _, back = c.foreign(pkt, browser)
	assert.False(t, back, "a packet on the way it came was noted as a move")
}

// TestAPathMoveSendsAgainWhatTheBrowserMayHaveLost moves the QUIC path of
// a browser hotspot to another replica. The replica must send again, in
// order, what it sent to the hotspot in the last hub.MoveSlack, and no
// more.
func TestAPathMoveSendsAgainWhatTheBrowserMayHaveLost(t *testing.T) {
	t.Parallel()
	s := queueServer()
	s.outgoingChan = make(chan outgoing, 16)
	ep := endpoint{transport: webTransportName, addr: wtAddr("a")}
	s.connected.Store(1, ep)
	now := time.Now()
	for i := range 12 {
		s.deliver(context.Background(), 1, models.Packet{StreamID: 7, Seq: uint(i)})
		s.writeOut(<-s.outgoingChan)
	}
	s.sentMu.Lock()
	for i, p := range s.sent[1] {
		p.at = now.Add(time.Duration(i-11)*60*time.Millisecond + 5*time.Millisecond)
	}
	s.sentMu.Unlock()
	s.resendMoved(wtAddr("a"), now)
	var got []uint
	for range 9 {
		select {
		case o := <-s.outgoingChan:
			assert.Equal(t, ep, endpointOf(o.raw))
			p, ok := models.UnpackPacket(o.raw.Data)
			require.True(t, ok)
			got = append(got, p.Seq)
		case <-time.After(time.Second):
			t.Fatalf("only %v were sent again", got)
		}
	}
	assert.Equal(t, []uint{3, 4, 5, 6, 7, 8, 9, 10, 11}, got)
	select {
	case o := <-s.outgoingChan:
		t.Fatalf("sent again a packet from before the window: %v", o.raw)
	case <-time.After(50 * time.Millisecond):
	}
}
