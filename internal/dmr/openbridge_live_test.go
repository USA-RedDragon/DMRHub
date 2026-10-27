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
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// liveSource is the radio of the packets that
	// checkForgedPacketsAreDropped makes.
	liveSource = 3000003
	// liveRemoteHotspot is the hotspot on the external server. FreeDMR
	// sends private data to a hotspot whose ID starts with the 7 digits of
	// the target (bridge_master.py, "User Peer Hotspot ID matched").
	liveRemoteHotspot = 300000201
	// liveRemoteRadio is the radio on liveRemoteHotspot.
	liveRemoteRadio = 3000002
)

func liveVoice(t *testing.T, c call) transmission {
	t.Helper()
	return voicePayload().build(t, c)[0]
}

func liveSMS(t *testing.T, c call) transmission {
	t.Helper()
	return smsPayload("sms", sms.FormatIP, "OPENBRIDGE TEST", enums.DataTypeRate12).build(t, c)[0]
}

// crossLive sends the transmission tx from one repeater in real time, one
// burst every 60 ms, as the external servers take it, and checks that
// every burst reaches the other.
func crossLive(t *testing.T, from, to *testutils.MMDVMClient, tx transmission, stream uint, name string) {
	t.Helper()
	for _, p := range tx.packets(stream) {
		require.NoError(t, from.SendDMRD(p))
		time.Sleep(60 * time.Millisecond)
	}
	got := ofCall(to.Drain(3*time.Second), uint(tx.src), uint(tx.dst))
	assert.Len(t, got, len(tx.bursts), "%s: every packet of the call arrives", name)
}

// checkForgedPacketsAreDropped sends the hub OpenBridge packets for the
// link of peer from another socket: one signed with another passphrase, one
// with a network ID that is no peer of the hub, and for an enhanced link
// one whose send time is 6 s old, which the hub must also quench. None may
// reach local, which hears TG mxTG; a good packet sent the same way must,
// so that the others differ from it only where they are bad.
func checkForgedPacketsAreDropped(t *testing.T, hubAddr string, peer uint32, password string, enhanced bool, local *testutils.MMDVMClient) {
	t.Helper()
	to, err := net.ResolveUDPAddr("udp", hubAddr)
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	stream := uint(0x7F00)
	send := func(network uint32, key string, age time.Duration) uint {
		stream++
		p := makeGroupVoicePacket(liveSource, mxTG, stream, false)
		p.Repeater = uint(network)
		b := dmrd.Bridge{Version: 1, Data: p.Data()}
		b.Data.Size = dmrconst.MMDVMPacketLength
		if enhanced {
			b = dmrd.Bridge{Version: 5, Data: p.Data(), Time: uint64(time.Now().Add(-age).UnixNano()), SourceServer: network, Hops: 1} //nolint:gosec
			b.Data.Size = dmrconst.MMDVMMaxPacketLength
		}
		out, err := b.AppendBinary(nil)
		require.NoError(t, err)
		require.NoError(t, dmrdauth.NewBridgeKey([]byte(key)).SignInPlace(out))
		_, err = conn.WriteToUDP(out, to)
		require.NoError(t, err)
		return stream
	}
	heard := func(stream uint) bool {
		for _, p := range local.Drain(2 * time.Second) {
			if p.StreamID == stream && p.Src == liveSource {
				return true
			}
		}
		return false
	}

	assert.False(t, heard(send(peer, password+"x", 0)), "a packet with a bad hash reached the repeater")
	assert.False(t, heard(send(3000999, password, 0)), "a packet with a wrong network ID reached the repeater")
	if enhanced {
		stale := send(peer, password, 6*time.Second)
		assert.False(t, heard(stale), "a stale packet reached the repeater")
		quenched := false
		buf := make([]byte, 512)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
		for !quenched {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				break
			}
			c, err := dmrd.DecodeBridgeControl(buf[:n])
			quenched = err == nil && c.Command == dmrd.CommandBCSQ && c.Stream == uint32(stale) //nolint:gosec
		}
		assert.True(t, quenched, "the hub sent no BCSQ for the stale stream")
	}
	assert.True(t, heard(send(peer, password, 0)), "a good packet sent the same way did not reach the repeater")
}
