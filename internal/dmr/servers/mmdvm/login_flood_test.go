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
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func rptl(id uint32) []byte {
	p := make([]byte, 8)
	copy(p, "RPTL")
	binary.BigEndian.PutUint32(p[4:], id)
	return p
}

// TestALoginFloodDoesNotDelayARealLogin floods the server with logins of
// IDs that are not known repeaters, whose lookups never end, and then logs
// a known repeater in once maxMisses of them wait. The clock of the server
// stands still, so no more of them start. The real login must get its
// RPTACK while they wait, within the LoginTimeoutDefault of a repeater.
func TestALoginFloodDoesNotDelayARealLogin(t *testing.T) {
	t.Parallel()
	const realID = 311870
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	var flooding atomic.Bool
	var stuck atomic.Int32
	entered := make(chan struct{}, 256)
	gate := make(chan struct{})
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:stuck", func(tx *gorm.DB) {
		if flooding.Load() && tx.Statement.Table == "repeaters" && !slices.Contains(tx.Statement.Vars, any(uint(realID))) {
			stuck.Add(1)
			entered <- struct{}{}
			<-gate
		}
	}))
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: realID},
		Type:                  models.RepeaterTypeMMDVM,
		Password:              "s3cret",
	}).Error)
	s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)
	s.clock = clock.NewFake(time.Now())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	t.Cleanup(func() { close(gate) })
	addr, ok := s.Addr().(*net.UDPAddr)
	require.True(t, ok)

	flooding.Store(true)
	for i := range 40 {
		c, err := net.DialUDP("udp", nil, addr)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		for j := range 5 {
			_, err := c.Write(rptl(uint32(400000 + i*10 + j))) //nolint:gosec
			require.NoError(t, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), dmrdsession.LoginTimeoutDefault)
	defer cancel()
	for range maxMisses {
		select {
		case <-entered:
		case <-ctx.Done():
			require.FailNow(t, "the lookups of the flood did not start", "%d of %d", stuck.Load(), maxMisses)
		}
	}

	c, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Write(rptl(realID))
	require.NoError(t, err)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(dmrdsession.LoginTimeoutDefault)))
	buf := make([]byte, 64)
	for {
		n, err := c.Read(buf)
		require.NoError(t, err, "no answer to a real login within the login timeout of a repeater, after a flood of 200 logins")
		if bytes.HasPrefix(buf[:n], []byte("RPTACK")) {
			break
		}
	}
	require.Equal(t, int32(maxMisses), stuck.Load(), "lookups of unknown IDs that started")
}
