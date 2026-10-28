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

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
)

// TestStopUnderAFlood stops the server while its socket has more packets
// than its queue holds. Stop must not wait for the reader of the socket.
func TestStopUnderAFlood(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(nil, kvStore, nil, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })

	s, err := MakeServer(&cfg, h, nil, nil, kvStore, "test", "test")
	require.NoError(t, err)
	require.NoError(t, s.Start(context.Background()))

	s.mu.Lock()
	addr, ok := s.Server.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	c, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	for range 4 * channelBufferSize {
		_, err := c.Write([]byte("RPTPING\x00\x00\x00\x01"))
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { return len(s.incomingChan) == cap(s.incomingChan) }, 2*time.Second, time.Millisecond)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = s.Stop(context.Background())
	}()
	time.Sleep(50 * time.Millisecond)
	s.mu.Unlock()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop waits for the reader of the socket")
	}
}

// TestSendPacketAfterStop sends to a repeater after the writer of the
// server stopped. The send must give up instead of waiting on the queue.
func TestSendPacketAfterStop(t *testing.T) {
	t.Parallel()
	s := Server{
		clock:        clock.Real(),
		connected:    xsync.NewMap[uint, endpoint](),
		outgoingChan: make(chan outgoing, 1),
		done:         make(chan struct{}),
	}
	s.connected.Store(1, udpEndpoint(net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}))
	s.outgoingChan <- outgoing{}
	close(s.done)

	sent := make(chan struct{})
	go func() {
		defer close(sent)
		s.sendPacket(context.Background(), 1, models.Packet{Signature: "DMRD"})
	}()
	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("sendPacket waits on the queue of a stopped server")
	}
}
