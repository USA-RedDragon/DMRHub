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

	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rpto returns an RPTO of the repeater id that is n octets long. A session
// that is not logged in answers one that it takes with MSTNAK.
func rpto(t *testing.T, id uint32, n int) []byte {
	t.Helper()
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTO, ID: id, Text: []byte("TS1=8")}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	for len(p) < n {
		p = append(p, ';')
	}
	return p
}

// TestOversizePacketsAreDropped sends packets of the longest length that
// the sessions take and one octet longer, on the UDP port and over
// WebTransport, the longer one first. The server must drop the longer
// one, rather than handle its first MaxPacket octets, so its first answer
// is to the other.
func TestOversizePacketsAreDropped(t *testing.T) {
	t.Parallel()
	s, cfg := webTransportServer(t)

	udp, err := net.DialUDP("udp", nil, s.Addr().(*net.UDPAddr)) //nolint:forcetypeassert
	require.NoError(t, err)
	t.Cleanup(func() { _ = udp.Close() })
	udpSend := func(p []byte) {
		_, err := udp.Write(p)
		require.NoError(t, err)
	}
	udpReply := func(p []byte) []byte {
		udpSend(p)
		require.NoError(t, udp.SetReadDeadline(time.Now().Add(answerWait)))
		buf := make([]byte, 2*dmrdsession.MaxPacket)
		n, err := udp.Read(buf)
		if err != nil {
			return nil
		}
		return buf[:n]
	}

	sess := dialByHash(t, s, cfg)
	wtSend := func(p []byte) { require.NoError(t, sess.SendDatagram(p)) }
	wtReply := func(p []byte) []byte {
		wtSend(p)
		ctx, cancel := context.WithTimeout(context.Background(), answerWait)
		defer cancel()
		b, err := sess.ReceiveDatagram(ctx)
		if err != nil {
			return nil
		}
		return b
	}

	send := map[string]func([]byte){"udp": udpSend, "webtransport": wtSend}
	for name, reply := range map[string]func([]byte) []byte{"udp": udpReply, "webtransport": wtReply} {
		send[name](rpto(t, 311861, dmrdsession.MaxPacket+1))
		got := reply(rpto(t, 311860, dmrdsession.MaxPacket))
		require.NotNil(t, got, "%s: a packet of MaxPacket octets is handled", name)
		k, err := dmrd.DecodeControl(got, dmrd.DialectMMDVM)
		require.NoError(t, err, name)
		assert.Equal(t, dmrd.CommandMSTNAK, k.Command, name)
		assert.Equal(t, uint32(311860), k.ID, "%s: a longer packet is dropped, so the first answer is to the packet after it", name)
	}
}
