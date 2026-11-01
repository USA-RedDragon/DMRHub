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
	"net"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAStopSendsWhatTheHubQueuedForTheServer stops the holder of the egress
// lease while the hub has packets queued for it, as when the hub routed the
// last bursts of a call just before the stop. Each of them must still go
// to the peer.
func TestAStopSendsWhatTheHubQueuedForTheServer(t *testing.T) {
	t.Parallel()
	stopWithQueued(t, false)
}

// TestAStopSendsWhatTheHubQueuedAroundABarrier stops the holder of the
// egress lease while the hub has packets queued for it with a Barrier
// among them, as when the hub settles a moved repeater just before the
// stop. The Barrier must not end the stop early: each packet before and
// after it must still go to the peer.
func TestAStopSendsWhatTheHubQueuedAroundABarrier(t *testing.T) {
	t.Parallel()
	stopWithQueued(t, true)
}

func stopWithQueued(t *testing.T, barrier bool) {
	t.Helper()
	const peerID, queued = 500002, 40
	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peerConn.Close() })
	at, ok := peerConn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	read := make(chan int, 1)
	go func() {
		buf := make([]byte, 512)
		n := 0
		for {
			m, _, err := peerConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if string(buf[:m]) == "end" {
				read <- n
				return
			}
			n++
		}
	}()

	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.DMR.OpenBridge.Bind = "127.0.0.1"
	cfg.DMR.OpenBridge.Port = 0
	h := hub.NewHub(nil, nil, nil, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })
	s, err := MakeServer(&cfg, h, nil, nil, nil)
	require.NoError(t, err)
	peer := &knownPeer{Peer: models.Peer{ID: peerID, Password: "password", IP: at.IP.String(), Port: at.Port, Egress: true}}
	s.peers = lookup.New(clock.Real(), "test peers", lookup.TTL, func(struct{}) (peerTable, error) {
		return peerTable{byID: map[uint]*knownPeer{peerID: peer}, all: []*knownPeer{peer}}, nil
	})
	s.role.Store(roleLeader)
	packets := make(chan hub.RoutedPacket, queued+1)
	for i := range queued {
		if barrier && i == queued/2 {
			packets <- hub.RoutedPacket{Barrier: make(chan struct{})}
		}
		packets <- hub.RoutedPacket{RepeaterID: peerID, Packet: models.Packet{Signature: "DMRD", Seq: uint(i), Src: 1, Dst: 1, GroupCall: true, StreamID: 9}}
	}
	close(packets)
	s.hubHandle = &hub.ServerHandle{Name: "openbridge", Packets: packets}
	s.wg.Add(1)
	go s.consumeHubPackets(context.Background())
	require.NoError(t, s.Stop(context.Background()))

	end, err := net.DialUDP("udp", nil, at)
	require.NoError(t, err)
	t.Cleanup(func() { _ = end.Close() })
	_, err = end.Write([]byte("end"))
	require.NoError(t, err)
	assert.Equal(t, queued, <-read, "the stop dropped packets that the hub queued for the server")
}
