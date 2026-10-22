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

package dmr_test

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func truncatedPackets(id uint32) [][]byte {
	var full [4]byte
	binary.BigEndian.PutUint32(full[:], id)
	var out [][]byte
	add := func(name string, maxLen int) {
		body := append([]byte(name), full[:]...)
		for len(body) < maxLen {
			body = append(body, byte(len(body)))
		}
		for n := 0; n <= maxLen; n++ {
			out = append(out, append([]byte(nil), body[:n]...))
		}
	}
	add("DMRA", 19)
	add("DMRG", 22)
	add("RPTC", 12)
	add("RPTCL", 8)
	add("RPTL", 7)
	add("RPTK", 12)
	add("RPTO", 12)
	add("RPTPING", 14)
	add("RPTG", 12)
	add("RPTRSSI", 14)
	add("RPTINTR", 14)
	add("DMRD", 54)
	return out
}

// TestTruncatedPacketsDoNotCrash sends every MMDVM packet type cut at each
// length, from a logged-in repeater and from an unknown address. v1.2.41
// panicked on the 15-octet DMRA that DMRGateway sends and on a 4-octet
// RPTC. The server must stay up and keep routing.
func TestTruncatedPacketsDoNotCrash(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))

		raddr, err := net.ResolveUDPAddr("udp", stack.MMDVMAddr)
		require.NoError(t, err)
		stranger, err := net.DialUDP("udp", nil, raddr)
		require.NoError(t, err)
		defer func() { _ = stranger.Close() }()

		for _, p := range truncatedPackets(100001) {
			require.NoError(t, c1.SendRaw(p))
			_, err := stranger.Write(p)
			require.NoError(t, err)
		}
		time.Sleep(settleDuration)
		_ = c2.Drain(100 * time.Millisecond)

		require.NoError(t, c1.SendDMRD(makeGroupVoicePacket(1000001, 1, 6001, false)))
		require.NoError(t, c1.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 6001, false)))
		assert.Len(t, c2.Drain(drainWait), 2, "the server must keep routing after truncated packets")
	})
}
