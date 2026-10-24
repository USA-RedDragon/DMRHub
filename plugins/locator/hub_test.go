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

package locator_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/plugins/locator"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/ars"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const repeater = 319186801

func packetOf(b *pluginv1.Burst) models.Packet {
	return models.Packet{
		Signature: string(dmrconst.CommandDMRD), Seq: uint(b.GetSequence()), Src: uint(b.GetSource()), Dst: uint(b.GetDestination()),
		Repeater: repeater, Slot: b.GetSlot() == 2, FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(b.GetDataTypeOrVoiceSequence()),
		StreamID: uint(b.GetStreamId()), DMRData: [33]byte(b.GetPayload()), BER: -1, RSSI: -1,
	}
}

// atRepeater collects the bursts that the hub sends to the repeater.
func atRepeater(t *testing.T, srv *hub.ServerHandle) func() []*pluginv1.Burst {
	t.Helper()
	var mu sync.Mutex
	var got []*pluginv1.Burst
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case rp := <-srv.Packets:
				if rp.RepeaterID == repeater {
					mu.Lock()
					got = append(got, plugins.ToBurst(&rp.Packet))
					mu.Unlock()
				}
			}
		}
	}()
	return func() []*pluginv1.Burst {
		mu.Lock()
		defer mu.Unlock()
		return append([]*pluginv1.Burst(nil), got...)
	}
}

// TestTheLocatorTracksARadioThroughTheHub has a tracked radio register on
// a repeater and report its position, and checks that the location plugin,
// connected over TLS, answers it there and that the hub keeps the position.
func TestTheLocatorTracksARadioThroughTheHub(t *testing.T) {
	t.Parallel()
	h, database, ps, srv := testutils.RepeaterHub(t, radio, repeater)
	require.NoError(t, models.SetPositionTracking(database, radio, radio, true, time.Now().Add(-time.Minute)))
	var mu sync.Mutex
	var notified []models.Position
	listener := testutils.StartPlugins(t, database, ps, h, func(r *plugins.Registry) { h.SetPlugins(r) }, func(p models.Position) {
		mu.Lock()
		defer mu.Unlock()
		notified = append(notified, p)
	})
	listener.Run(t, "Location", []models.PluginRange{{First: 5050, Last: 5059}}, locator.Serve)
	sent := atRepeater(t, srv)

	for _, b := range radioBursts(t, registration(t, ars.TypeDeviceRegistration), false) {
		h.RoutePacket(context.Background(), packetOf(b), models.RepeaterTypeMMDVM)
		time.Sleep(60 * time.Millisecond)
	}
	var got []answer
	require.Eventually(t, func() bool {
		got = answers(t, sent())
		return len(got) >= 2
	}, 10*time.Second, 50*time.Millisecond)
	require.NotNil(t, got[0].ars)
	assert.Equal(t, ars.TypeAck, got[0].ars.Header.Type)
	require.NotNil(t, got[1].lrrp)
	assert.Equal(t, lrrp.DocumentTriggeredRequest, got[1].lrrp.Document)
	assert.Equal(t, uint32(600), got[1].lrrp.Request.Interval)
	for _, b := range sent() {
		assert.Equal(t, uint32(service), b.GetSource())
		assert.Equal(t, uint32(2), b.GetSlot())
	}

	for _, b := range radioBursts(t, report(t, 30.2672, -97.7431, 7), false) {
		h.RoutePacket(context.Background(), packetOf(b), models.RepeaterTypeMMDVM)
		time.Sleep(60 * time.Millisecond)
	}
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(notified) == 1
	}, 5*time.Second, 20*time.Millisecond)
	var stored []models.Position
	require.NoError(t, database.Find(&stored).Error)
	require.Len(t, stored, 1)
	p := stored[0]
	assert.Equal(t, uint(radio), p.RadioID)
	assert.InDelta(t, 30.2672, p.Latitude, 1e-4)
	assert.Equal(t, uint(repeater), p.RepeaterID)
	assert.Equal(t, uint(service), p.ServiceID)
	assert.Equal(t, uint8(2), p.Slot)
}
