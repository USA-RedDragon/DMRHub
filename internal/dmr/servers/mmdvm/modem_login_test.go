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
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

func TestALoginWithTheIDOfAModemIsRefused(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
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
	const modemID, networkID = 311871, 311872
	for id, typ := range map[uint]string{modemID: models.RepeaterTypeModem, networkID: models.RepeaterTypeMMDVM} {
		require.NoError(t, database.Create(&models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: id},
			Type:                  typ,
			Password:              "s3cret",
		}).Error)
	}
	s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	addr, ok := s.Addr().(*net.UDPAddr)
	require.True(t, ok)

	answer := func(id uint32) string {
		c, err := net.DialUDP("udp", nil, addr)
		require.NoError(t, err)
		defer func() { _ = c.Close() }()
		_, err = c.Write(rptl(id))
		require.NoError(t, err)
		require.NoError(t, c.SetReadDeadline(time.Now().Add(3*time.Second)))
		buf := make([]byte, 64)
		for {
			n, err := c.Read(buf)
			require.NoError(t, err)
			for _, tag := range []string{"RPTACK", "MSTNAK"} {
				if bytes.HasPrefix(buf[:n], []byte(tag)) {
					return tag
				}
			}
		}
	}
	require.Equal(t, "RPTACK", answer(networkID))
	require.Equal(t, "MSTNAK", answer(modemID), "a network login took the ID of the modem")
}
