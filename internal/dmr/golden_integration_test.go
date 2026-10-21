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
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The captures in testdata come from dmrgo transport/dmrd testdata, made on
// 127.0.0.1 on 2026-10-20:
//
// dmrgateway-master.txt: DMRGateway ee79d38 (r>m) logs in to a master (m>r)
// with the password passw0rd and sends one superframe on slot 2 from
// 3191868 to talkgroup 91.
//
// hblink3-openbridge.txt: HBlink3 1db2730 (h>o) sends the same call on
// slot 1 over an OpenBridge link with network ID 3129100 and the passphrase
// passw0rd-longer-than-twenty.
type capturePacket struct {
	dir string
	p   []byte
}

func readCapture(t *testing.T, path string) []capturePacket {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var out []capturePacket
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 2 {
			continue
		}
		p, err := hex.DecodeString(fs[len(fs)-1])
		require.NoError(t, err)
		dir := fs[0]
		if len(fs) == 4 {
			dir = fs[1]
		}
		out = append(out, capturePacket{dir: dir, p: p})
	}
	require.NoError(t, sc.Err())
	return out
}

func readUDPPacket(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	buf := make([]byte, 1500)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(handshakeWait)))
	n, err := conn.Read(buf)
	require.NoError(t, err)
	return buf[:n]
}

// assertSameCall checks that DMRHub delivered the DMRD packets of the
// capture with the call fields and bursts as sent.
func assertSameCall(t *testing.T, want [][]byte, got []models.Packet, slot2 bool) {
	t.Helper()
	require.Len(t, got, len(want))
	for i, p := range want {
		w, ok := models.UnpackPacket(p[:53])
		require.True(t, ok)
		g := got[i]
		assert.Equal(t, w.Seq, g.Seq, "packet %d seq", i)
		assert.Equal(t, w.Src, g.Src, "packet %d src", i)
		assert.Equal(t, w.Dst, g.Dst, "packet %d dst", i)
		assert.Equal(t, slot2, g.Slot, "packet %d slot", i)
		assert.Equal(t, w.GroupCall, g.GroupCall, "packet %d group", i)
		assert.Equal(t, w.FrameType, g.FrameType, "packet %d frame type", i)
		assert.Equal(t, w.DTypeOrVSeq, g.DTypeOrVSeq, "packet %d data type or position", i)
		assert.Equal(t, w.StreamID, g.StreamID, "packet %d stream", i)
		assert.Equal(t, w.DMRData, g.DMRData, "packet %d burst", i)
	}
}

// TestGoldenDMRGatewayLoginAndCall replays the login and call of
// DMRGateway. DMRHub must answer each login step with the octets the master
// of the capture sent, and deliver the call to a repeater on talkgroup 91.
// DMRHub does not answer RPTO, so that step has no reply.
func TestGoldenDMRGatewayLoginAndCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const gatewayID = 0x3010AF
		capture := readCapture(t, "testdata/dmrgateway-master.txt")

		stack.SeedUser(t, 3191868, "KI5VMF")
		stack.SeedTalkgroup(t, 91, "TG91")
		stack.SeedMMDVMRepeater(t, gatewayID, 3191868, "passw0rd")
		stack.SeedMMDVMRepeater(t, 100001, 3191868, testPassword)
		stack.AssignTS2StaticTG(t, 100001, 91)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))

		addr, err := net.ResolveUDPAddr("udp", stack.MMDVMAddr)
		require.NoError(t, err)
		gw, err := net.DialUDP("udp", nil, addr)
		require.NoError(t, err)
		defer func() { _ = gw.Close() }()

		// RPTL: the master answers RPTACK with its salt.
		_, err = gw.Write(capture[0].p)
		require.NoError(t, err)
		reply := readUDPPacket(t, gw)
		require.Len(t, reply, 10)
		require.Equal(t, "RPTACK", string(reply[:6]))
		salt := binary.BigEndian.Uint32(reply[6:])

		// RPTK: the token of the capture is that of its salt; send the token
		// of the salt DMRHub picked.
		capturedSalt := binary.BigEndian.Uint32(capture[1].p[6:])
		token, err := auth.Token(dmrd.DialectMMDVM, capturedSalt, []byte("passw0rd"))
		require.NoError(t, err)
		require.Equal(t, capture[2].p[8:40], token[:])
		token, err = auth.Token(dmrd.DialectMMDVM, salt, []byte("passw0rd"))
		require.NoError(t, err)
		rptk := append([]byte(nil), capture[2].p...)
		copy(rptk[8:], token[:])
		_, err = gw.Write(rptk)
		require.NoError(t, err)
		require.Equal(t, capture[3].p, readUDPPacket(t, gw))

		// RPTC
		_, err = gw.Write(capture[4].p)
		require.NoError(t, err)
		require.Equal(t, capture[5].p, readUDPPacket(t, gw))

		// RPTO
		_, err = gw.Write(capture[6].p)
		require.NoError(t, err)
		time.Sleep(settleDuration)

		var call [][]byte
		var ping, pong []byte
		for _, c := range capture {
			switch {
			case c.dir == "r>m" && strings.HasPrefix(string(c.p), "DMRD"):
				call = append(call, c.p)
			case c.dir == "r>m" && strings.HasPrefix(string(c.p), "RPTPING"):
				ping = c.p
			case c.dir == "m>r" && strings.HasPrefix(string(c.p), "MSTPONG"):
				pong = c.p
			}
		}
		require.Len(t, call, 8)
		for _, p := range call {
			_, err = gw.Write(p)
			require.NoError(t, err)
			time.Sleep(60 * time.Millisecond)
		}
		assertSameCall(t, call, mc.Drain(drainWait), true)

		_, err = gw.Write(ping)
		require.NoError(t, err)
		require.Equal(t, pong, readUDPPacket(t, gw))
	})
}

// TestGoldenHBlink3OpenBridgeIngress replays the OpenBridge packets of
// HBlink3. DMRHub must check their signatures with the passphrase cut at 20
// octets, as HBlink3 signs them, and deliver the call to a repeater.
func TestGoldenHBlink3OpenBridgeIngress(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const networkID = 3129100
		capture := readCapture(t, "testdata/hblink3-openbridge.txt")
		require.Len(t, capture, 8)

		stack.SeedUser(t, 3191868, "KI5VMF")
		stack.SeedTalkgroup(t, 91, "TG91")
		stack.SeedMMDVMRepeater(t, 100001, 3191868, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 91)
		stack.SeedPeer(t, networkID, 3191868, "passw0rd-longer-than-twenty", "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, networkID, true, 91, 91)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		addr, err := net.ResolveUDPAddr("udp", stack.OpenBridgeAddr)
		require.NoError(t, err)
		hb, err := net.DialUDP("udp", nil, addr)
		require.NoError(t, err)
		defer func() { _ = hb.Close() }()

		call := make([][]byte, 0, len(capture))
		for _, c := range capture {
			call = append(call, c.p)
			_, err = hb.Write(c.p)
			require.NoError(t, err)
			time.Sleep(60 * time.Millisecond)
		}
		assertSameCall(t, call, mc.Drain(drainWait), false)
	})
}

// TestGoldenHBlink3OpenBridgeEgress sends the call of the HBlink3 capture
// from a repeater. DMRHub must send each packet to the OpenBridge peer with
// the octets HBlink3 sent, signature included.
func TestGoldenHBlink3OpenBridgeEgress(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const networkID = 3129100
		capture := readCapture(t, "testdata/hblink3-openbridge.txt")
		require.Len(t, capture, 8)

		stack.SeedUser(t, 3191868, "KI5VMF")
		stack.SeedTalkgroup(t, 91, "TG91")
		stack.SeedMMDVMRepeater(t, 100001, 3191868, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 91)
		stack.StartServers(t)

		peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		require.NoError(t, err)
		defer func() { _ = peer.Close() }()
		peerAddr, ok := peer.LocalAddr().(*net.UDPAddr)
		require.True(t, ok)
		stack.SeedPeer(t, networkID, 3191868, "passw0rd-longer-than-twenty", "127.0.0.1", peerAddr.Port, false, true)
		stack.SeedPeerRule(t, networkID, false, 3191868, 3191868)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		defer mc.Close()
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		for _, c := range capture {
			pkt, ok := models.UnpackPacket(c.p[:53])
			require.True(t, ok)
			require.NoError(t, mc.SendDMRD(pkt))
			time.Sleep(60 * time.Millisecond)
		}
		for i, c := range capture {
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(drainWait)))
			buf := make([]byte, 1500)
			n, err := peer.Read(buf)
			require.NoError(t, err, "packet %d", i)
			assert.Equal(t, hex.EncodeToString(c.p), hex.EncodeToString(buf[:n]), "packet %d", i)
		}
	})
}
