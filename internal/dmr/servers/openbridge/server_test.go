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
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeServer(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}

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

	server, err := openbridge.MakeServer(&defConfig, nil, database, ps, kvStore)
	assert.NoError(t, err)
	defer func() { _ = server.Server.Close() }()

	assert.NotNil(t, server.Buffer)
	assert.Len(t, server.Buffer, 73) // largestMessageSize
	assert.NotNil(t, server.DB)
}

func TestMakeServerDefaultBindAddress(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
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

	server, err := openbridge.MakeServer(&defConfig, nil, database, ps, kvStore)
	assert.NoError(t, err)
	defer func() { _ = server.Server.Close() }()

	assert.Equal(t, 62035, server.SocketAddress.Port)
	assert.Equal(t, "0.0.0.0", server.SocketAddress.IP.String())
}

// TestUDPBufferCopyRegression verifies that the UDP read loop copies data from
// the shared buffer before spawning the processing goroutine. Without the copy,
// a subsequent ReadFromUDP would overwrite the buffer while the goroutine is
// still marshalling the previous packet, causing data corruption.
func TestUDPBufferCopyRegression(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
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

	ct := calltracker.NewCallTracker(database, ps)
	dmrHub := hub.NewHub(database, kvStore, ps, ct)

	server, err := openbridge.MakeServer(&defConfig, dmrHub, database, ps, kvStore)
	require.NoError(t, err)
	defer func() { _ = server.Server.Close() }()

	// Subscribe to the incoming topic before Start so we catch all messages
	sub := ps.Subscribe("openbridge:incoming")
	defer func() { _ = sub.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = server.Start(ctx)
	require.NoError(t, err)

	// Get the actual listening address (port was 0, so OS assigned one)
	localAddr, ok := server.Server.LocalAddr().(*net.UDPAddr)
	require.True(t, ok, "expected *net.UDPAddr from LocalAddr()")

	// Send N distinct 73-byte packets rapidly. Each packet has a unique
	// fill byte so we can verify data integrity on the receiving side.
	const numPackets = 20
	const packetSize = 73
	for i := 0; i < numPackets; i++ {
		pkt := make([]byte, packetSize)
		// Fill with a unique byte value per packet
		for j := range pkt {
			pkt[j] = byte(i + 1) // 1..numPackets (avoid 0 to distinguish from zero-value)
		}
		conn, dialErr := net.DialUDP("udp", nil, localAddr)
		require.NoError(t, dialErr)
		_, writeErr := conn.Write(pkt)
		require.NoError(t, writeErr)
		_ = conn.Close()
	}

	// Collect all published messages with a timeout
	received := make([]models.RawDMRPacket, 0, numPackets)
	timeout := time.After(5 * time.Second)
	for len(received) < numPackets {
		select {
		case msg, ok := <-sub.Channel():
			if !ok {
				t.Fatal("subscription channel closed unexpectedly")
			}
			var raw models.RawDMRPacket
			_, err := raw.UnmarshalMsg(msg)
			require.NoError(t, err)
			received = append(received, raw)
		case <-timeout:
			t.Fatalf("timed out waiting for packets: got %d of %d", len(received), numPackets)
		}
	}

	// Verify every received packet has homogeneous fill bytes (no corruption
	// from a subsequent read overwriting the shared buffer).
	for i, raw := range received {
		require.Len(t, raw.Data, packetSize, "packet %d has wrong length", i)
		fillByte := raw.Data[0]
		require.NotZero(t, fillByte, "packet %d has zero fill byte", i)
		for j, b := range raw.Data {
			assert.Equal(t, fillByte, b,
				"packet %d byte %d: expected 0x%02x but got 0x%02x (buffer was overwritten)",
				i, j, fillByte, b)
		}
	}
}
