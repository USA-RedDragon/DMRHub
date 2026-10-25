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
// before it moves. The repeater has it, so the handover must not hold it.
func TestHandOverNoPacketSentBeforeTheMove(t *testing.T) {
	t.Parallel()
	s := queueServer()
	s.deliver(context.Background(), 1, models.Packet{StreamID: 7, Seq: 3})
	s.writeOut(<-s.outgoingChan)
	time.Sleep(time.Millisecond)
	assert.Empty(t, s.sentSince(1, time.Now()))
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
