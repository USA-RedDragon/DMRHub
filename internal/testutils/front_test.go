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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	n, from, err := c.ReadFromUDP(buf)
	require.NoError(t, err)
	return string(buf[:n]), from
}

// TestUDPFrontPassesOnWhatAMovedSessionReceived moves the session of a
// client while the front holds what the draining backend sent it and has
// not passed on yet, as when the front is slow under load. The client gets
// all of it, then what the new backend sends, and nothing that the old
// backend sends after the move.
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

	want := []string{"1", "2", "3", "4", "5", "new"}
	sent := want[:5]
	f.mu.Lock()
	for _, p := range sent {
		_, err := oldBackend.WriteToUDP([]byte(p), oldUp)
		require.NoError(t, err)
	}
	f.find(oldBackend.LocalAddr().String()).draining = true
	clientAddr, ok := client.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	moved := f.sessionLocked(clientAddr)
	require.NotNil(t, moved)
	f.mu.Unlock()
	_, err = moved.up.WriteToUDP([]byte("move"), moved.backend.addr)
	require.NoError(t, err)
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
