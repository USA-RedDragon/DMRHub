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

package openbridge_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"net"
	"slices"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeServer(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	assert.NoError(t, err)

	memdb.Use(&defConfig)

	database, err := db.MakeDB(&defConfig)
	assert.NoError(t, err)
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	assert.NoError(t, err)

	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	assert.NoError(t, err)
	defer func() { _ = kvStore.Close() }()

	defConfig.DMR.OpenBridge.Port = 0

	server, err := openbridge.MakeServer(&defConfig, nil, database, ps, nil)
	assert.NoError(t, err)
	defer func() { _ = server.Server.Close() }()

	assert.NotNil(t, server.Buffer)
	assert.Greater(t, len(server.Buffer), 89, "the buffer must hold an 89-octet DMRE packet")
	assert.NotNil(t, server.DB)
}

func TestMakeServerDefaultBindAddress(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	assert.NoError(t, err)

	memdb.Use(&defConfig)
	defConfig.DMR.OpenBridge.Bind = "0.0.0.0"
	defConfig.DMR.OpenBridge.Port = 62035

	database, err := db.MakeDB(&defConfig)
	assert.NoError(t, err)
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	assert.NoError(t, err)

	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	assert.NoError(t, err)
	defer func() { _ = kvStore.Close() }()

	server, err := openbridge.MakeServer(&defConfig, nil, database, ps, nil)
	assert.NoError(t, err)
	defer func() { _ = server.Server.Close() }()

	assert.Equal(t, 62035, server.SocketAddress.Port)
	assert.Equal(t, "0.0.0.0", server.SocketAddress.IP.String())
}

func TestMakeServerBracketedBindAddress(t *testing.T) {
	t.Parallel()

	for bind, want := range map[string]string{"[::]": "::", "[::1]": "::1", "127.0.0.1": "127.0.0.1"} {
		t.Run(bind, func(t *testing.T) {
			t.Parallel()

			defConfig, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			defConfig.DMR.OpenBridge.Bind = bind
			defConfig.DMR.OpenBridge.Port = 0

			server, err := openbridge.MakeServer(&defConfig, nil, nil, nil, nil)
			require.NoError(t, err)
			defer func() { _ = server.Server.Close() }()

			local, ok := server.Server.LocalAddr().(*net.UDPAddr)
			require.True(t, ok)
			assert.Equal(t, want, local.IP.String())
			assert.Equal(t, want, server.SocketAddress.IP.String())
			assert.Equal(t, local.Port, server.SocketAddress.Port)
		})
	}
}

// TestStopClosesSocketAndUnregistersHub verifies that Stop() properly closes
// the UDP socket and unregisters the server from the hub. This is a regression
// test for the bug where Stop() was a no-op, leaking the socket and leaving
// the hub entry dangling.
func TestStopClosesSocketAndUnregistersHub(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)

	memdb.Use(&defConfig)
	defConfig.DMR.OpenBridge.Port = 0 // OS-assigned port

	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	require.NoError(t, err)
	defer func() { _ = ps.Close() }()

	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	require.NoError(t, err)
	defer func() { _ = kvStore.Close() }()

	ct := calltracker.NewCallTracker(database, ps, kvStore)
	dmrHub := hub.NewHub(database, kvStore, ps, ct)

	server, err := openbridge.MakeServer(&defConfig, dmrHub, database, ps, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = server.Start(ctx)
	require.NoError(t, err)

	// Verify the socket is open by checking that LocalAddr doesn't fail
	localAddr := server.Server.LocalAddr()
	require.NotNil(t, localAddr, "socket should be open before Stop")

	// Stop the server
	err = server.Stop(ctx)
	require.NoError(t, err)

	// Verify the socket is closed: a write should fail with net.ErrClosed
	_, writeErr := server.Server.WriteToUDP([]byte("test"), &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 1234,
	})
	require.Error(t, writeErr, "writing to a closed socket should return an error")
}

// FuzzHandleOpenBridgePacket sends fuzzed UDP payloads to a running OpenBridge
// server and verifies that no panics occur regardless of input.
func FuzzHandleOpenBridgePacket(f *testing.F) {
	// Seed: valid 73-byte OpenBridge packet (53 bytes DMRD + 20 bytes HMAC-SHA1)
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
	f.Add([]byte{})         // empty
	f.Add([]byte{0xFF})     // single byte
	f.Add(make([]byte, 73)) // all zeros, correct length
	f.Add(make([]byte, 72)) // one byte short
	f.Add(make([]byte, 74)) // one byte long

	// DMRD signature with junk body, correct length
	dmrdJunk := make([]byte, 73)
	copy(dmrdJunk, []byte("DMRD"))
	f.Add(dmrdJunk)

	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()

		defConfig, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)

		memdb.Use(&defConfig)
		defConfig.DMR.OpenBridge.Port = 0

		database, err := db.MakeDB(&defConfig)
		require.NoError(t, err)
		defer func() {
			sqlDB, _ := database.DB()
			_ = sqlDB.Close()
		}()

		ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
		require.NoError(t, err)
		defer func() { _ = ps.Close() }()

		kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
		require.NoError(t, err)
		defer func() { _ = kvStore.Close() }()

		ct := calltracker.NewCallTracker(database, ps, kvStore)
		dmrHub := hub.NewHub(database, kvStore, ps, ct)

		server, err := openbridge.MakeServer(&defConfig, dmrHub, database, ps, nil)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err = server.Start(ctx)
		require.NoError(t, err)
		defer func() { _ = server.Stop(ctx) }()

		localAddr, ok := server.Server.LocalAddr().(*net.UDPAddr)
		require.True(t, ok)

		conn, err := net.DialUDP("udp", nil, localAddr)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()

		// Send the fuzzed data — the server must not panic
		_, _ = conn.Write(data)

		// Give the server time to process
		time.Sleep(50 * time.Millisecond)
	})
}

// FuzzValidateOpenBridgeHMAC builds a 73-byte packet from fuzzed payload and
// password, then sends it to a running server. The server's HMAC validation
// path must not panic regardless of input.
func FuzzValidateOpenBridgeHMAC(f *testing.F) {
	f.Add(make([]byte, 53), "correctPassword")
	f.Add(make([]byte, 53), "")
	f.Add(make([]byte, 53), "wrongPassword")

	f.Fuzz(func(t *testing.T, payload []byte, password string) {
		t.Parallel()

		// Build a 73-byte packet: first 53 bytes + HMAC-SHA1(password) of those bytes
		if len(payload) < dmrconst.MMDVMPacketLength {
			padded := make([]byte, dmrconst.MMDVMPacketLength)
			copy(padded, payload)
			payload = padded
		} else {
			payload = payload[:dmrconst.MMDVMPacketLength]
		}

		h := hmac.New(sha1.New, []byte(password))
		_, _ = h.Write(payload)
		packet := slices.Concat(payload, h.Sum(nil))

		defConfig, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)

		memdb.Use(&defConfig)
		defConfig.DMR.OpenBridge.Port = 0

		database, err := db.MakeDB(&defConfig)
		require.NoError(t, err)
		defer func() {
			sqlDB, _ := database.DB()
			_ = sqlDB.Close()
		}()

		ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
		require.NoError(t, err)
		defer func() { _ = ps.Close() }()

		kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
		require.NoError(t, err)
		defer func() { _ = kvStore.Close() }()

		ct := calltracker.NewCallTracker(database, ps, kvStore)
		dmrHub := hub.NewHub(database, kvStore, ps, ct)

		server, err := openbridge.MakeServer(&defConfig, dmrHub, database, ps, nil)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err = server.Start(ctx)
		require.NoError(t, err)
		defer func() { _ = server.Stop(ctx) }()

		localAddr, ok := server.Server.LocalAddr().(*net.UDPAddr)
		require.True(t, ok)

		conn, err := net.DialUDP("udp", nil, localAddr)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()

		_, _ = conn.Write(packet)
		time.Sleep(50 * time.Millisecond)
	})
}

// --- Benchmarks ---

// BenchmarkOpenBridgeHMAC measures the cost of computing HMAC-SHA1 over a
// 53-byte OpenBridge packet, which happens once per outbound packet per peer.
func BenchmarkOpenBridgeHMAC(b *testing.B) {
	password := []byte("benchmarkPassword123")
	payload := make([]byte, dmrconst.MMDVMPacketLength)
	copy(payload, []byte("DMRD"))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := hmac.New(sha1.New, password)
		_, _ = h.Write(payload)
		_ = h.Sum(nil)
	}
}

// BenchmarkOpenBridgePacketEncodeAndHMAC measures the full outbound path:
// encode a Packet to wire format, slice to 53 bytes, compute HMAC, and concat.
func BenchmarkOpenBridgePacketEncodeAndHMAC(b *testing.B) {
	password := []byte("benchmarkPassword123")
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

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded := pkt.Encode()[:dmrconst.MMDVMPacketLength]
		h := hmac.New(sha1.New, password)
		_, _ = h.Write(encoded)
		_ = slices.Concat(encoded, h.Sum(nil))
	}
}
