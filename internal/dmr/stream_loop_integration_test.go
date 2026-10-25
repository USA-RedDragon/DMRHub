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

package dmr_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamLoopIsDropped builds the loop that HBlink4 made with TG91 on its
// outbound link and on its OpenBridge link to the hub: a hotspot and an
// OpenBridge peer both on TG1 send every burst they get straight back, under
// the same stream ID. The hub must carry the talker's call once and drop the
// copies, which come from other sources.
func TestStreamLoopIsDropped(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const stream = 7001
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100003, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.AssignTS1StaticTG(t, 100003, 1)
		stack.StartServers(t)

		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		defer obc.Close()
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))
		addr := obc.LocalAddr()
		stack.SeedPeer(t, 500001, 1000001, obPassword, addr.IP.String(), addr.Port, true, true)
		stack.SeedPeerRule(t, 500001, true, 1, 1)
		stack.SeedPeerRule(t, 500001, false, 1000001, 1000001)

		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		looper := testutils.NewMMDVMClient(100002, "", testPassword)
		listener := testutils.NewMMDVMClient(100003, "", testPassword)
		for _, c := range []*testutils.MMDVMClient{talker, looper, listener} {
			require.NoError(t, c.Connect(stack.MMDVMAddr))
			defer c.Close()
			require.NoError(t, c.WaitReady(handshakeWait))
		}
		time.Sleep(settleDuration)

		ctx, cancel := context.WithCancel(context.Background())
		var mu sync.Mutex
		got := map[string]int{}
		var wg sync.WaitGroup
		echo := func(name string, ch <-chan models.Packet, send func(models.Packet) error) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case p := <-ch:
					if p.StreamID != stream {
						continue
					}
					mu.Lock()
					got[name]++
					mu.Unlock()
					if send != nil {
						_ = send(p)
					}
				}
			}
		}
		wg.Add(3)
		go echo("looper", looper.ReceivedPackets(), looper.SendDMRD)
		go echo("peer", obc.ReceivedPackets(), func(p models.Packet) error {
			p.Repeater = 500001
			return obc.SendRawPacket(p)
		})
		go echo("listener", listener.ReceivedPackets(), nil)

		metrics.NewMetrics()
		before := loopDrops(t)

		call := []models.Packet{
			makeBurst(1000001, 1, stream, true, dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader), 0),
			makeGroupVoicePacket(1000001, 1, stream, false),
			makeGroupVoiceTermPacket(1000001, 1, stream, false),
		}
		for _, p := range call {
			require.NoError(t, talker.SendDMRD(p))
			time.Sleep(60 * time.Millisecond)
		}
		time.Sleep(drainWait)
		cancel()
		wg.Wait()

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, len(call), got["listener"], "the listener must get each burst of the call once")
		assert.Equal(t, len(call), got["looper"], "the looping hotspot must get each burst once, not the peer's copies")
		assert.Equal(t, len(call), got["peer"], "the OpenBridge peer must get each burst once, not the hotspot's copies")
		assert.GreaterOrEqual(t, loopDrops(t)-before, float64(2*len(call)), "the looped copies must be counted")
		assert.Empty(t, talker.Drain(100*time.Millisecond), "the talker must not get its own call back")

		var calls []models.Call
		require.NoError(t, stack.DB.Where("stream_id = ?", stream).Find(&calls).Error)
		require.Len(t, calls, 1, "the call must be tracked once")
		require.NotNil(t, calls[0].RepeaterID)
		assert.Equal(t, uint(100001), *calls[0].RepeaterID)
	})
}

func loopDrops(t *testing.T) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() != "dmr_ingress_dropped_bursts_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "reason" && l.GetValue() == metrics.DropReasonStreamFromOtherSource {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// TestStreamLoopIsDroppedAcrossReplicas builds the loop of
// TestStreamLoopIsDropped over two replicas: the talker and the listener are
// on one replica, and the hotspot that sends every burst straight back is on
// the other. The replica of the looping hotspot must drop the copies, so the
// listener gets the call once.
func TestStreamLoopIsDroppedAcrossReplicas(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const stream = 7002
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100003, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.AssignTS1StaticTG(t, 100003, 1)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		looper := testutils.NewMMDVMClient(100002, "", testPassword)
		listener := testutils.NewMMDVMClient(100003, "", testPassword)
		for c, addr := range map[*testutils.MMDVMClient]string{talker: stack.MMDVMAddr, looper: second.MMDVMAddr, listener: stack.MMDVMAddr} {
			require.NoError(t, c.Connect(addr))
			defer c.Close()
			require.NoError(t, c.WaitReady(handshakeWait))
		}
		time.Sleep(settleDuration)

		ctx, cancel := context.WithCancel(context.Background())
		var mu sync.Mutex
		got := map[string]int{}
		var wg sync.WaitGroup
		echo := func(name string, ch <-chan models.Packet, send func(models.Packet) error) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case p := <-ch:
					if p.StreamID != stream {
						continue
					}
					mu.Lock()
					got[name]++
					mu.Unlock()
					if send != nil {
						_ = send(p)
					}
				}
			}
		}
		wg.Add(3)
		go echo("looper", looper.ReceivedPackets(), looper.SendDMRD)
		go echo("listener", listener.ReceivedPackets(), nil)
		go echo("talker", talker.ReceivedPackets(), nil)

		call := []models.Packet{
			makeBurst(1000001, 1, stream, true, dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader), 0),
			makeGroupVoicePacket(1000001, 1, stream, false),
			makeGroupVoiceTermPacket(1000001, 1, stream, false),
		}
		for _, p := range call {
			require.NoError(t, talker.SendDMRD(p))
			time.Sleep(60 * time.Millisecond)
		}
		time.Sleep(drainWait)
		cancel()
		wg.Wait()

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, len(call), got["looper"], "the looping hotspot must get each burst once")
		assert.Equal(t, len(call), got["listener"], "the listener must get each burst once, not the looped copies")
		assert.Zero(t, got["talker"], "the talker must not get its own call back")

		var calls []models.Call
		require.NoError(t, stack.DB.Where("stream_id = ?", stream).Find(&calls).Error)
		require.Len(t, calls, 1, "the call must be tracked once")
	})
}
