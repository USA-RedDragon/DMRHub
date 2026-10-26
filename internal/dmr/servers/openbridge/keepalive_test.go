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
	"sync/atomic"
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
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestKeepalivesDoNotReadThePeersEachTick(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = ""
	cfg.Database.ExtraParameters = []string{}
	cfg.DMR.OpenBridge.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = kvStore.Close() })

	far, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = far.Close() })
	farAddr, ok := far.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	owner := models.User{ID: 1000001, Callsign: "N0CALL", Username: "n0call", Approved: true}
	require.NoError(t, database.Create(&owner).Error)
	require.NoError(t, database.Create(&models.Peer{ID: 3100100, OwnerID: owner.ID, Password: "x", IP: "127.0.0.1", Port: farAddr.Port, Enhanced: true}).Error)

	var reads atomic.Int64
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:count_peers", func(tx *gorm.DB) {
		if tx.Statement.Table == "peers" {
			reads.Add(1)
		}
	}))

	ct := calltracker.NewCallTracker(database, ps, kvStore)
	t.Cleanup(ct.Stop)
	server, err := openbridge.MakeServer(&cfg, hub.NewHub(database, kvStore, ps, ct), database, ps, nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, server.Start(ctx))
	t.Cleanup(func() { _ = server.Stop(context.Background()) })

	keepalives := 0
	buf := make([]byte, 512)
	deadline := time.Now().Add(3500 * time.Millisecond)
	for time.Now().Before(deadline) {
		require.NoError(t, far.SetReadDeadline(deadline))
		if _, _, err := far.ReadFromUDP(buf); err != nil {
			break
		}
		keepalives++
	}
	assert.Positive(t, keepalives, "keepalives the peer got")
	assert.LessOrEqual(t, reads.Load(), int64(1), "reads of the peers in 3.5 s of keepalives")
}
