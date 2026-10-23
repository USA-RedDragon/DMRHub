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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

// TestStopWithFullIngressQueue stops the server while its ingress queue is
// full, as when a busy hub falls behind. The reader of the socket must
// not stay blocked on the queue after the listener has stopped, or Stop
// waits forever.
func TestStopWithFullIngressQueue(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = ""
	cfg.Database.ExtraParameters = []string{}
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })

	s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)
	require.NoError(t, s.Start(context.Background()))

	s.mu.Lock()
	conn, err := net.DialUDP("udp", nil, s.Server.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	for range 4 * channelBufferSize {
		_, _ = conn.Write([]byte("RPTPING\x00\x00\x00\x01"))
	}
	require.Eventually(t, func() bool { return len(s.incomingChan) == cap(s.incomingChan) }, 5*time.Second, 10*time.Millisecond)

	stopped := make(chan struct{})
	go func() {
		_ = s.Stop(context.Background())
		close(stopped)
	}()
	time.Sleep(100 * time.Millisecond)
	s.mu.Unlock()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return within 5s with a full ingress queue")
	}
}
