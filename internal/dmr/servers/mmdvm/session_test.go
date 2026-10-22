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
	"errors"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
)

func TestPendingSessionsAreCapped(t *testing.T) {
	t.Parallel()
	var s Server
	for i := range maxPending {
		_, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 0, byte(i>>8), byte(i)), Port: 62031})
		if err != nil {
			t.Fatalf("link %d: %v", i, err)
		}
	}
	if _, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 1, 0, 0), Port: 62031}); !errors.Is(err, errTooManyPending) {
		t.Fatalf("got %v, want errTooManyPending", err)
	}
	if _, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 0, 0, 0), Port: 62031}); err != nil {
		t.Fatalf("known address: %v", err)
	}
}

func TestHalfOpenLoginEndsAfterLoginTimeout(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = ""
	cfg.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 311860},
		Type:                  models.RepeaterTypeMMDVM,
		Password:              "s3cret",
	}).Error)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	s := Server{
		DB:        database,
		Server:    conn,
		kvClient:  servers.MakeKVClient(kvStore),
		connected: xsync.NewMap[uint, net.UDPAddr](),
	}
	rptl, err := (&dmrd.Control{Command: dmrd.CommandRPTL, ID: 311860}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	from := net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 62031}
	s.handlePacket(context.Background(), from, rptl)
	require.Len(t, s.links, 1)
	require.Equal(t, dmrdsession.StateAuth, s.links[from.String()].m.State())

	s.epoch = s.epoch.Add(-(dmrdsession.LoginTimeoutDefault - time.Second))
	s.tick(context.Background())
	require.Len(t, s.links, 1, "the login should still be open before LoginTimeout")

	s.epoch = s.epoch.Add(-2 * time.Second)
	s.tick(context.Background())
	require.Empty(t, s.links, "the half-open login should end after LoginTimeout")
}
