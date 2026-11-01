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

package openbridge

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"encoding/binary"
	"net"
	"slices"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	"github.com/stretchr/testify/require"
)

type fuzzServer struct {
	*Server
	ctx context.Context //nolint:containedctx
}

// startFuzzServer starts an OpenBridge server on a :0 port with its own
// in-memory database, pubsub, KV store and hub.
func startFuzzServer(t *testing.T) fuzzServer {
	t.Helper()
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&defConfig)
	defConfig.DMR.OpenBridge.Port = 0

	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	ps, err := pubsub.MakePubSub(t.Context(), &defConfig)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	kvStore, err := kv.MakeKV(t.Context(), &defConfig)
	require.NoError(t, err)
	t.Cleanup(func() { _ = kvStore.Close() })

	ct := calltracker.NewCallTracker(database, ps, kvStore)
	dmrHub := hub.NewHub(database, kvStore, ps, ct)
	server, err := MakeServer(&defConfig, dmrHub, database, ps, nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, server.Start(ctx))
	t.Cleanup(func() {
		_ = server.Stop(ctx)
		cancel()
	})
	return fuzzServer{Server: &server, ctx: ctx}
}

// deliver runs the packet handler of the server on data, as the read loop
// does for a datagram, and returns when the handler does. The read loop
// runs the handler on the ingress lane of the packet, which only orders
// the packets of a lane; the fuzz target sends one packet at a time, so
// calling the handler directly adds no wait and no order that the read
// loop does not have.
func (s fuzzServer) deliver(t *testing.T, data []byte) {
	t.Helper()
	from, ok := s.Server.Server.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	s.handlePacket(s.ctx, from, data)
}

// FuzzHandleOpenBridgePacket hands fuzzed payloads to the packet handler of a
// running OpenBridge server and verifies that no panics occur regardless of
// input.
func FuzzHandleOpenBridgePacket(f *testing.F) {
	password := "testpass"
	pkt := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         1,
		Src:         1000001,
		Dst:         1,
		Repeater:    500001,
		GroupCall:   true,
		Slot:        false,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA,
		StreamID:    42,
		BER:         -1,
		RSSI:        -1,
	}
	encoded := pkt.Encode()[:dmrconst.MMDVMPacketLength]
	h := hmac.New(sha1.New, []byte(password))
	_, _ = h.Write(encoded)
	validPacket := slices.Concat(encoded, h.Sum(nil))

	f.Add(validPacket)
	f.Add([]byte{})
	f.Add([]byte{0xFF})
	f.Add(make([]byte, 73))
	f.Add(make([]byte, 72))
	f.Add(make([]byte, 74))

	dmrdJunk := make([]byte, 73)
	copy(dmrdJunk, []byte("DMRD"))
	f.Add(dmrdJunk)

	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()
		startFuzzServer(t).deliver(t, data)
	})
}

// FuzzValidateOpenBridgeHMAC builds a 73-byte DMRD packet of a known peer
// from a fuzzed payload and passphrase, and hands it to the packet handler
// of a running server twice: signed with the passphrase of the peer, and
// signed with another one. The server must not panic, must take the first
// and must drop the second at its HMAC check. As in HBlink3, the key is the
// passphrase padded with zero octets and cut at dmrdauth.KeyLen.
func FuzzValidateOpenBridgeHMAC(f *testing.F) {
	f.Add(make([]byte, 53), "correctPassword")
	f.Add(make([]byte, 53), "")
	f.Add(make([]byte, 53), "wrongPassword")

	const peerID = 500001
	f.Fuzz(func(t *testing.T, payload []byte, password string) {
		t.Parallel()

		data := make([]byte, dmrconst.MMDVMPacketLength)
		copy(data, payload)
		copy(data, dmrconst.CommandDMRD)
		binary.BigEndian.PutUint32(data[11:15], peerID)
		data[15] &^= 0x80

		key := make([]byte, dmrdauth.KeyLen)
		copy(key, password)
		other := slices.Clone(key)
		other[0] ^= 1
		sign := func(key []byte) []byte {
			h := hmac.New(sha1.New, key)
			_, _ = h.Write(data)
			return slices.Concat(data, h.Sum(nil))
		}

		s := startFuzzServer(t)
		require.NoError(t, s.DB.Create(&models.Peer{ID: peerID, Password: password, Ingress: true}).Error)

		s.deliver(t, sign(other))
		_, pinged := s.pinged.Load(peerID)
		require.False(t, pinged, "a packet signed with another passphrase passed the HMAC check")

		s.deliver(t, sign(key))
		_, pinged = s.pinged.Load(peerID)
		require.True(t, pinged, "a packet signed with the passphrase of the peer failed the HMAC check")
	})
}
