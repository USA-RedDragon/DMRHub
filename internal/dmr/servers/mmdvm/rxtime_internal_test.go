//go:build linux

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
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPacketIsStampedWhenItArrives sends a packet that waits in the
// socket before the reader takes it, as when the replica is slow to run
// its reader. The packet must carry the time it came, not the time the
// reader took it: a repeater that moves here is handed what its old
// replica sent after that time, and a late stamp loses what went out in
// between.
func TestAPacketIsStampedWhenItArrives(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	s, err := MakeServer(&cfg, nil, nil, nil, kvStore, "test", "test")
	require.NoError(t, err)
	t.Cleanup(func() {
		close(s.done)
		_ = s.Server.Close()
		s.wg.Wait()
	})

	addr, ok := s.Server.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	c, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	ping := make([]byte, 11)
	copy(ping, "RPTPING")
	binary.BigEndian.PutUint32(ping[7:], 100001)
	sent := time.Now()
	_, err = c.Write(ping)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	s.wg.Add(1)
	go s.serve(udpTransport, s.Server, s.Buffer)
	select {
	case p := <-s.incomingChan:
		assert.Less(t, p.ReceivedAt.Sub(sent), 100*time.Millisecond, "the packet is stamped when the reader takes it")
	case <-time.After(2 * time.Second):
		t.Fatal("the packet did not come")
	}
}
