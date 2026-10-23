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
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func rptl(id uint32) []byte {
	p := make([]byte, 8)
	copy(p, "RPTL")
	binary.BigEndian.PutUint32(p[4:], id)
	return p
}

func TestALoginFloodDoesNotDelayARealLogin(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:flood_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	var slow atomic.Bool
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:slow", func(tx *gorm.DB) {
		if slow.Load() && tx.Statement.Table == "repeaters" {
			time.Sleep(100 * time.Millisecond)
		}
	}))
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })
	const realID = 311870
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: realID},
		Type:                  models.RepeaterTypeMMDVM,
		Password:              "s3cret",
	}).Error)
	s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	addr, ok := s.Addr().(*net.UDPAddr)
	require.True(t, ok)

	slow.Store(true)
	for i := range 40 {
		c, err := net.DialUDP("udp", nil, addr)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		for j := range 5 {
			_, err := c.Write(rptl(uint32(400000 + i*10 + j))) //nolint:gosec
			require.NoError(t, err)
		}
	}

	c, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	start := time.Now()
	_, err = c.Write(rptl(realID))
	require.NoError(t, err)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(3*time.Second)))
	buf := make([]byte, 64)
	for {
		n, err := c.Read(buf)
		require.NoError(t, err, "no answer to a real login within 3 s of a flood of 200 logins")
		if bytes.HasPrefix(buf[:n], []byte("RPTACK")) {
			break
		}
	}
	t.Logf("a real login was answered %s after a flood of 200 logins", time.Since(start))
}
