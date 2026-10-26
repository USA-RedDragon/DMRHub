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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func queueServer() *Server {
	s := &Server{
		connected:    xsync.NewMap[uint, endpoint](),
		transports:   xsync.NewMap[string, transport](),
		outgoingChan: make(chan outgoing, 4),
		done:         make(chan struct{}),
	}
	s.connected.Store(1, udpEndpoint(net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}))
	return s
}

// TestHandOverAPacketQueuedAtTheMove queues a packet to a repeater that
// then moves. The packet leaves after the move, which the load balancer no
// longer takes to the repeater, so the handover must hold it.
func TestHandOverAPacketQueuedAtTheMove(t *testing.T) {
	t.Parallel()
	s := queueServer()
	s.deliver(context.Background(), 1, models.Packet{StreamID: 7, Seq: 3})
	time.Sleep(time.Millisecond)
	since := time.Now()
	got := s.sentSince(1, since)
	require.Len(t, got, 1, "the handover misses the packet that waits in the queue")
	assert.Equal(t, uint(3), got[0].Seq)
}

// TestHandOverNoPacketSentBeforeTheMove sends a packet to a repeater
// well before it moves. The repeater has it, so the handover must not hold
// it.
func TestHandOverNoPacketSentBeforeTheMove(t *testing.T) {
	t.Parallel()
	s := queueServer()
	s.deliver(context.Background(), 1, models.Packet{StreamID: 7, Seq: 3})
	s.writeOut(<-s.outgoingChan)
	assert.Empty(t, s.sentSince(1, time.Now().Add(2*hub.MoveSlack)))
}

// TestHandOverAPacketSentJustBeforeTheClaim sends a packet to a repeater
// a moment before the packet of the repeater reached the other replica.
// The load balancer moved the repeater when it got that packet, a little
// earlier, and may have dropped the packet to the repeater, so the
// handover must hold it.
func TestHandOverAPacketSentJustBeforeTheClaim(t *testing.T) {
	t.Parallel()
	s := queueServer()
	s.deliver(context.Background(), 1, models.Packet{StreamID: 7, Seq: 3})
	s.writeOut(<-s.outgoingChan)
	assert.Len(t, s.sentSince(1, time.Now().Add(100*time.Microsecond)), 1, "the handover misses the packet sent just before the claim")
}

// TestHandOverAPacketSentAfterTheMove sends a packet that was queued
// before the move after it. The handover must hold it.
func TestHandOverAPacketSentAfterTheMove(t *testing.T) {
	t.Parallel()
	s := queueServer()
	s.deliver(context.Background(), 1, models.Packet{StreamID: 7, Seq: 3})
	o := <-s.outgoingChan
	time.Sleep(time.Millisecond)
	since := time.Now()
	time.Sleep(time.Millisecond)
	s.writeOut(o)
	assert.Len(t, s.sentSince(1, since), 1)
}

// slowTransport is a transport whose writes wait until release closes.
type slowTransport struct {
	writing chan struct{}
	release chan struct{}
}

func (t *slowTransport) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, net.ErrClosed }

func (t *slowTransport) WriteTo(p []byte, _ net.Addr) (int, error) {
	close(t.writing)
	<-t.release
	return len(p), nil
}

func (t *slowTransport) Close() error { return nil }

// TestHandOverAPacketWrittenLate stalls the write of a packet to a
// repeater, as a writer that gets no CPU for a while does, past the move
// of the repeater. The packet leaves after the move, so the handover must
// hold it.
func TestHandOverAPacketWrittenLate(t *testing.T) {
	t.Parallel()
	s := queueServer()
	slow := &slowTransport{writing: make(chan struct{}), release: make(chan struct{})}
	s.transports.Store("slow", slow)
	s.connected.Store(1, endpoint{transport: "slow", addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}})
	s.deliver(context.Background(), 1, models.Packet{StreamID: 7, Seq: 3})
	o := <-s.outgoingChan
	wrote := make(chan struct{})
	go func() {
		defer close(wrote)
		s.writeOut(o)
	}()
	<-slow.writing
	time.Sleep(2 * hub.MoveSlack)
	got := s.sentSince(1, time.Now())
	close(slow.release)
	<-wrote
	assert.Len(t, got, 1, "the handover misses the packet that left after the move")
}
