//go:build linux

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
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPacketIsStampedWhenItArrives sends a packet that waits in the
// socket before the reader takes it, as when the replica is slow to run
// its reader. The packet must carry the time it came, not the time the
// reader took it: a repeater that moves here is handed what its old
// replica sent after that time, and a late stamp loses what went out in
// between.
func TestAPacketIsStampedWhenItArrives(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	s, err := MakeServer(&cfg, nil, nil, nil, kvStore, "test", "test")
	require.NoError(t, err)
	t.Cleanup(func() {
		close(s.done)
		_ = s.Server.Close()
		s.wg.Wait()
	})

	addr, ok := s.Server.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	c, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	ping := make([]byte, 11)
	copy(ping, "RPTPING")
	binary.BigEndian.PutUint32(ping[7:], 100001)
	awaitStamps(t, s.Server, c)
	_, err = c.Write(ping)
	require.NoError(t, err)

	reading := time.Now()
	s.wg.Add(1)
	go s.serve(udpTransport, s.Server, s.Buffer)
	ctx, cancel := context.WithTimeout(context.Background(), answerWait)
	defer cancel()
	select {
	case p := <-s.incomingChan:
		assert.True(t, p.ReceivedAt.Before(reading), "the packet is stamped when the reader takes it: %v, the reader started at %v", p.ReceivedAt, reading)
	case <-ctx.Done():
		t.Fatalf("the packet did not come in %v", answerWait)
	}
}

// awaitStamps waits until the kernel stamps the packets of c as they come:
// until a probe that is in the socket before the read carries a stamp from
// before the read. It turns the stamps on for every socket a moment after
// the first socket asks, and until then stamps a packet when it is read.
func awaitStamps(t *testing.T, c *net.UDPConn, peer *net.UDPConn) {
	t.Helper()
	if !servers.StampArrivals(c) {
		t.Fatal("the kernel does not stamp the packets")
	}
	buf := make([]byte, 1500)
	oob := make([]byte, servers.RxOOBLen)
	ctx, cancel := context.WithTimeout(context.Background(), answerWait)
	defer cancel()
	for ctx.Err() == nil {
		if _, err := peer.Write([]byte("probe")); err != nil {
			t.Fatalf("probe: %v", err)
		}
		reading := time.Now()
		if _, _, at, err := servers.ReadStamped(c, clock.Real(), true, buf, oob); err == nil && at.Before(reading) {
			return
		}
	}
	t.Fatal("the kernel does not stamp the packets")
}
