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

package ipsc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"golang.org/x/sys/unix"
)

// stampWait bounds the wait for the kernel to turn on the stamps, which it
// does on a work queue after the first socket asks.
const stampWait = 5 * time.Second

// TestAPacketIsStampedWhenItArrives sends a registration that waits in the
// socket before the reader takes it, as when the replica is slow to run
// its reader. The packet must carry the time it came, not the time the
// reader took it: a peer that moves here is handed what its old replica
// sent after that time, and a late stamp loses what went out in between.
func TestAPacketIsStampedWhenItArrives(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.DMR.IPSC.Bind = "127.0.0.1"
	s := NewIPSCServer(cfg, nil, nil)
	key := testKey(t)
	s.keyFor = func(uint32) ([20]byte, bool, error) { return key, true, nil }
	if err := s.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		s.stopped.Store(true)
		_ = s.udp.Close()
		s.wg.Wait()
	})
	srv, ok := s.udp.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	client, _ := dialServer(t, srv)
	awaitStamps(t, s.udp, client)
	if _, err := client.Write(signTestPacket(t, control(t, ipscwire.OpRegisterRequest, 7))); err != nil {
		t.Fatalf("write: %v", err)
	}

	awaitQueued(t, s.udp)
	took := time.Now()
	s.wg.Add(1)
	go s.handler(context.Background())
	if r := readUDP(t, client); ipscwire.Opcode(r[0]) != ipscwire.OpRegisterReply {
		t.Fatalf("register reply opcode 0x%02x", r[0])
	}
	s.mu.Lock()
	heard := s.heard
	s.mu.Unlock()
	if !heard.Before(took) {
		t.Fatalf("the packet is stamped at %v, when the reader took it, not before %v, when it was in the socket", heard, took)
	}
}

// awaitStamps waits until the kernel stamps the packets of c as they come.
// It turns the stamps on for every socket a moment after the first socket
// asks, and until then stamps a packet when it is read. A probe that is in
// the socket before the test reads it shows which stamp the kernel gave.
func awaitStamps(t *testing.T, c *net.UDPConn, peer *net.UDPConn) {
	t.Helper()
	if !servers.StampArrivals(c) {
		t.Fatal("the kernel does not stamp the packets")
	}
	buf := make([]byte, 1500)
	oob := make([]byte, servers.RxOOBLen)
	deadline := time.Now().Add(stampWait)
	for time.Now().Before(deadline) {
		if _, err := peer.Write([]byte("probe")); err != nil {
			t.Fatalf("probe: %v", err)
		}
		awaitQueued(t, c)
		queued := time.Now()
		if _, _, at, err := servers.ReadStamped(c, clock.Real(), true, buf, oob); err == nil && at.Before(queued) {
			return
		}
	}
	t.Fatalf("the kernel did not stamp the packets as they came within %v", stampWait)
}

// awaitQueued waits until a packet is in the socket c, without reading it.
func awaitQueued(t *testing.T, c *net.UDPConn) {
	t.Helper()
	raw, err := c.SyscallConn()
	if err != nil {
		t.Fatalf("syscall conn: %v", err)
	}
	if err := c.SetReadDeadline(time.Now().Add(eventWait)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var ierr error
	err = raw.Read(func(fd uintptr) bool {
		var n int
		n, ierr = unix.IoctlGetInt(int(fd), unix.SIOCINQ)
		return ierr != nil || n > 0
	})
	if err != nil || ierr != nil {
		t.Fatalf("wait for a packet in the socket: %v %v", err, ierr)
	}
	if err := c.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
}
