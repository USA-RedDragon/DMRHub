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

package testutils

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const replyWait = time.Minute

func listenLoopback(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func readUDP(t *testing.T, c *net.UDPConn) (string, *net.UDPAddr) {
	t.Helper()
	buf := make([]byte, 64)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(replyWait)))
	n, from, err := c.ReadFromUDP(buf)
	require.NoError(t, err)
	return string(buf[:n]), from
}

// TestUDPFrontPassesOnWhatAMovedSessionReceived moves the session of a
// client while the front holds what the draining backend sent it and has
// not passed on yet, as when the front is slow under load. The client gets
// all of it, and what the old backend sends until the front passed the
// packet that moved the client on, then what the new backend sends, and
// nothing that the old backend sends after.
func TestUDPFrontPassesOnWhatAMovedSessionReceived(t *testing.T) {
	t.Parallel()
	f := NewUDPFront(t)
	oldBackend, newBackend, client := listenLoopback(t), listenLoopback(t), listenLoopback(t)
	f.Add(t, oldBackend.LocalAddr().String())
	front, err := net.ResolveUDPAddr("udp", f.Addr())
	require.NoError(t, err)

	_, err = client.WriteToUDP([]byte("hello"), front)
	require.NoError(t, err)
	_, oldUp := readUDP(t, oldBackend)
	f.Add(t, newBackend.LocalAddr().String())

	want := []string{"1", "2", "3", "4", "5", "before", "new"}
	f.mu.Lock()
	for _, p := range want[:5] {
		_, err := oldBackend.WriteToUDP([]byte(p), oldUp)
		require.NoError(t, err)
	}
	f.find(oldBackend.LocalAddr().String()).draining = true
	f.mu.Unlock()
	clientAddr, ok := client.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	moved, old := f.sessionFor(clientAddr)
	require.NotNil(t, moved)
	require.NotNil(t, old)
	_, err = oldBackend.WriteToUDP([]byte("before"), oldUp)
	require.NoError(t, err)
	f.forward(moved, old, []byte("move"))
	f.endMove(old)
	_, newUp := readUDP(t, newBackend)
	_, err = newBackend.WriteToUDP([]byte("new"), newUp)
	require.NoError(t, err)

	got := make([]string, 0, len(want))
	for range want {
		p, _ := readUDP(t, client)
		got = append(got, p)
	}
	assert.Equal(t, want, got)

	_, _ = oldBackend.WriteToUDP([]byte("late"), oldUp)
	require.NoError(t, client.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	buf := make([]byte, 64)
	n, _, err := client.ReadFromUDP(buf)
	assert.ErrorIs(t, err, os.ErrDeadlineExceeded, "the client got %q after the move", buf[:n])
	assert.Equal(t, 1, f.Moves())
}

// TestUDPFrontDropsWhatTheOldBackendSentAfterTheMove moves the session of a
// client and has the old backend send a packet after the new backend got
// the packet that moved the client, but before the front ends the old
// session, as when the front is slow under load. The client never gets it,
// and the kernel stamped the moving packet at the new backend no earlier
// than the cut of the old session, so a handover from that stamp holds
// every packet that the front drops.
func TestUDPFrontDropsWhatTheOldBackendSentAfterTheMove(t *testing.T) {
	t.Parallel()
	f := NewUDPFront(t)
	oldBackend, newBackend, client := listenLoopback(t), listenLoopback(t), listenLoopback(t)
	if !servers.StampArrivals(newBackend) {
		t.Skip("the kernel does not stamp when packets come")
	}
	f.Add(t, oldBackend.LocalAddr().String())
	front, err := net.ResolveUDPAddr("udp", f.Addr())
	require.NoError(t, err)

	_, err = client.WriteToUDP([]byte("hello"), front)
	require.NoError(t, err)
	_, oldUp := readUDP(t, oldBackend)
	f.Add(t, newBackend.LocalAddr().String())
	f.Drain(oldBackend.LocalAddr().String())

	clientAddr, ok := client.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	moved, old := f.sessionFor(clientAddr)
	require.NotNil(t, moved)
	require.NotNil(t, old)
	_, err = oldBackend.WriteToUDP([]byte("before"), oldUp)
	require.NoError(t, err)
	f.forward(moved, old, []byte("move"))
	require.NoError(t, newBackend.SetReadDeadline(time.Now().Add(replyWait)))
	buf := make([]byte, 64)
	_, newUp, stamped, err := servers.ReadStamped(newBackend, clock.Real(), true, buf, make([]byte, servers.RxOOBLen))
	require.NoError(t, err)
	f.mu.Lock()
	cut := old.cut
	f.mu.Unlock()
	if cut.IsZero() {
		t.Skip("the kernel does not stamp when packets leave")
	}
	assert.False(t, stamped.Before(cut), "the new backend got the moving packet at %s, before the cut at %s", stamped, cut)
	_, err = oldBackend.WriteToUDP([]byte("after"), oldUp)
	require.NoError(t, err)
	f.endMove(old)
	_, err = newBackend.WriteToUDP([]byte("new"), newUp)
	require.NoError(t, err)

	got := make([]string, 0, 2)
	for range 2 {
		p, _ := readUDP(t, client)
		got = append(got, p)
	}
	assert.Equal(t, []string{"before", "new"}, got)
}
