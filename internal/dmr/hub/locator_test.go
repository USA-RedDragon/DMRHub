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

package hub_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/plugins/locator"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/ars"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func confirmedReport(t *testing.T, src, dst, repeater uint) []models.Packet {
	t.Helper()
	m := lrrp.Message{Document: lrrp.DocumentUnsolicitedReport}
	m.Report.Circle2D = lrrp.Circle2D{Latitude: 30.2672, Longitude: -97.7431, Radius: 10}
	m.Report.HasCircle2D = true
	payload, err := m.AppendBinary(nil)
	require.NoError(t, err)
	return dataCall(t, src, dst, repeater, ip.PortLRRP, payload, true)
}

func arsRegistration(t *testing.T, src, dst, repeater uint) []models.Packet {
	t.Helper()
	m := ars.NewRegistration(ars.EventInitial)
	buf, err := m.SetDeviceID(nil, uint32(src))
	require.NoError(t, err)
	n := len(buf)
	buf, err = m.AppendBinary(buf)
	require.NoError(t, err)
	return dataCall(t, src, dst, repeater, ip.PortARS, buf[n:], false)
}

func dataCall(t *testing.T, src, dst, repeater uint, port ip.Port, payload []byte, confirmed bool) []models.Packet {
	t.Helper()
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, uint32(src)),
		Destination:     ip.RadioAddress(ip.DefaultNetwork, uint32(dst)),
		SourcePort:      port,
		DestinationPort: port,
		TTL:             64,
		Payload:         payload,
	}
	packet, err := ip.AppendIPv4(nil, &d)
	require.NoError(t, err)
	var h pdu.DataHeader
	if confirmed {
		require.NoError(t, h.SetConfirmed(pdu.ConfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: uint32(dst), LLIDSource: uint32(src), FullMessageFlag: true}))
	} else {
		require.NoError(t, h.SetUnconfirmed(pdu.UnconfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: uint32(dst), LLIDSource: uint32(src), FullMessageFlag: true}))
	}
	tx, err := data.NewSender(&data.SenderConfig{ColorCode: 1, Source: layer2.SyncSourceMS, Slot: enums.Slot2, ResponseWait: time.Second})
	require.NoError(t, err)
	require.NoError(t, tx.Send(0, &data.Message{Header: h, Rate: enums.DataTypeRate12, Payload: packet}))
	var outs []data.Output
	var pkts []models.Packet
	for now := time.Duration(0); now < 3*time.Second && tx.State() != data.SenderWaitResponse && (confirmed || tx.State() != data.SenderIdle || len(pkts) == 0); now += 60 * time.Millisecond {
		outs = tx.Tick(now, outs[:0])
		for _, o := range outs {
			if o.Kind == data.OutputBurst {
				pkts = append(pkts, models.Packet{
					Signature: string(dmrconst.CommandDMRD), Seq: uint(len(pkts)), Src: src, Dst: dst,
					Repeater: repeater, Slot: true, FrameType: dmrconst.FrameDataSync,
					DTypeOrVSeq: uint(o.Hint.DataType), StreamID: 0xD47B, DMRData: o.Burst, BER: -1, RSSI: -1,
				})
			}
		}
	}
	require.NotEmpty(t, pkts)
	return pkts
}

// locatorHub returns lastSeenHub with the location plugin at 5050 to
// 5059.
func locatorHub(t *testing.T) (*hub.Hub, *gorm.DB, *hub.ServerHandle) {
	t.Helper()
	h, database, ps, srv := lastSeenHubWithPubSub(t)
	listener := testutils.StartPlugins(t, database, ps, h, func(r *plugins.Registry) { h.SetPlugins(r) }, nil)
	listener.Run(t, "Location", []models.PluginRange{{First: 5050, Last: 5059}}, locator.Serve)
	return h, database, srv
}

const (
	answerQuiet       = 500 * time.Millisecond
	registrationQuiet = 3 * time.Second
	foreignQuiet      = 3500 * time.Millisecond
	voiceEvery        = 300 * time.Millisecond
	maxPackets        = 100
)

func nextFrom(srv *hub.ServerHandle, d time.Duration) (hub.RoutedPacket, bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case rp := <-srv.Packets:
		return rp, true
	case <-timer.C:
		return hub.RoutedPacket{}, false
	}
}

func TestDataToTheLocationServerIsAnsweredOnItsRepeater(t *testing.T) {
	t.Parallel()
	h, _, srv := locatorHub(t)

	for _, p := range confirmedReport(t, 1000002, 5057, 100003) {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
	rp, ok := nextFrom(srv, failAfter)
	require.True(t, ok, "the location server did not answer a confirmed report")
	assert.Equal(t, uint(100003), rp.RepeaterID, "the answer goes out where the report came in")
	assert.Equal(t, uint(5057), rp.Packet.Src)
	assert.Equal(t, uint(1000002), rp.Packet.Dst)
	assert.False(t, rp.Packet.GroupCall)
	if rp, ok := nextFrom(srv, answerQuiet); ok {
		t.Fatalf("only the answer goes out, but a packet went to %d", rp.RepeaterID)
	}
}

func TestASilentTrackedRadioIsAskedAgainWhereItIsHeard(t *testing.T) {
	t.Parallel()
	h, database, ps, srv := lastSeenHubWithPubSub(t)
	require.NoError(t, models.SetPositionTracking(database, 1000002, 1000002, true, time.Now()))
	require.NoError(t, database.Model(&models.PositionSetting{}).Where("radio_id = ?", 1000002).Update("interval_seconds", 1).Error)
	listener := testutils.StartPlugins(t, database, ps, h, func(r *plugins.Registry) { h.SetPlugins(r) }, nil)
	listener.Run(t, "Location", []models.PluginRange{{First: 5050, Last: 5059}}, locator.Serve)

	for _, p := range arsRegistration(t, 1000002, 5057, 100003) {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
	for n := 0; ; n++ {
		rp, ok := nextFrom(srv, registrationQuiet)
		if !ok {
			break
		}
		require.Equal(t, uint(100003), rp.RepeaterID)
		require.Less(t, n, maxPackets, "the Locator did not stop answering the registration")
	}

	foreign := makeVoicePacket(1, 70999, true, true)
	foreign.Src, foreign.Repeater = 1000002, 100004
	h.RoutePacket(context.Background(), foreign, models.RepeaterTypeMMDVM)
	if rp, ok := nextFrom(srv, foreignQuiet); ok {
		t.Fatalf("a sighting on another user's hotspot does not count, but a packet went to %d", rp.RepeaterID)
	}

	voice := makeVoicePacket(1, 71000, true, true)
	voice.Src, voice.Repeater = 1000002, 100003
	for range maxPackets {
		voice.Seq++
		h.RoutePacket(context.Background(), voice, models.RepeaterTypeMMDVM)
		if rp, ok := nextFrom(srv, voiceEvery); ok && rp.Packet.Src == 5057 {
			assert.Equal(t, uint(100003), rp.RepeaterID, "asked again where the radio is heard")
			return
		}
	}
	t.Fatal("a silent tracked radio that is heard was not asked again")
}
