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
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIPSCClientSettleCountsWhatCameBefore sends the client datagrams while
// its receive loop waits, so that Heard does not count them yet. Settle
// must return only once the client read all of them.
func TestIPSCClientSettleCountsWhatCameBefore(t *testing.T) {
	t.Parallel()
	master := listenLoopback(t)
	c := NewIPSCClient(200001, "")
	require.NoError(t, c.ConnectWithoutRegistration(master.LocalAddr().String()))
	t.Cleanup(c.Close)
	to, ok := c.conn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	c.mu.Lock()
	for range 3 {
		_, err := master.WriteToUDP([]byte{0x00}, to)
		require.NoError(t, err)
	}
	require.Zero(t, c.Heard(), "the client counted a datagram while its receive loop waited")
	c.mu.Unlock()

	require.NoError(t, c.Settle())
	require.Equal(t, 3, c.Heard())
}

func TestIPSCClientCountsOnlyTheDatagramsOfItsMaster(t *testing.T) {
	t.Parallel()
	master := listenLoopback(t)
	foreign := listenLoopback(t)
	c := NewIPSCClient(200001, "")
	require.NoError(t, c.ConnectWithoutRegistration(master.LocalAddr().String()))
	t.Cleanup(c.Close)
	to, ok := c.conn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	_, err := foreign.WriteToUDP([]byte{0x00}, to)
	require.NoError(t, err)
	require.NoError(t, c.Settle())
	require.Zero(t, c.Heard(), "the client counted a datagram from a socket that is not its master")

	_, err = master.WriteToUDP([]byte{0x00}, to)
	require.NoError(t, err)
	require.NoError(t, c.Settle())
	require.Equal(t, 1, c.Heard())
}
