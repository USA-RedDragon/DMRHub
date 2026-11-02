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

package main

import (
	"net"
	"testing"

	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeMaster(t *testing.T, answer dmrd.Command) *peer {
	t.Helper()
	m, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := m.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if dmrd.Classify(buf[:n], dmrd.DialectMMDVM) != dmrd.CommandRPTPING {
				continue
			}
			b, err := (&dmrd.Control{Command: answer, ID: 300000401}).AppendBinary(nil, dmrd.DialectMMDVM)
			if err == nil {
				_, _ = m.WriteToUDP(b, from)
			}
		}
	}()
	addr, ok := m.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	conn, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	p := newPeer(300000401, conn)
	t.Cleanup(p.close)
	return p
}

// TestAPingWaitsForThePong pings a hub that answers with MSTPONG: the
// ping returns once the hub answered it, so that the demo clock moves on
// only after the hub took the ping.
func TestAPingWaitsForThePong(t *testing.T) {
	t.Parallel()
	assert.NoError(t, fakeMaster(t, dmrd.CommandMSTPONG).ping())
}

// TestAPingThatTheHubRefusesFails pings a hub that ended the session of
// the repeater and answers with MSTNAK. The ping must fail at once, so that
// the shot names the repeater whose session ended instead of waiting for
// a ping that DMRHub never saves.
func TestAPingThatTheHubRefusesFails(t *testing.T) {
	t.Parallel()
	assert.ErrorContains(t, fakeMaster(t, dmrd.CommandMSTNAK).ping(), "refused")
}
