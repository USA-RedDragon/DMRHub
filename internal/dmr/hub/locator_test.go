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
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func confirmedReport(t *testing.T, src, dst, repeater uint) []models.Packet {
	t.Helper()
	m := lrrp.Message{Document: lrrp.DocumentUnsolicitedReport}
	m.Report.Circle2D = lrrp.Circle2D{Latitude: 30.2672, Longitude: -97.7431, Radius: 10}
	m.Report.HasCircle2D = true
	payload, err := m.AppendBinary(nil)
	require.NoError(t, err)
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, uint32(src)),
		Destination:     ip.RadioAddress(ip.DefaultNetwork, uint32(dst)),
		SourcePort:      ip.PortLRRP,
		DestinationPort: ip.PortLRRP,
		TTL:             64,
		Payload:         payload,
	}
	packet, err := ip.AppendIPv4(nil, &d)
	require.NoError(t, err)
	var h pdu.DataHeader
	require.NoError(t, h.SetConfirmed(pdu.ConfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: uint32(dst), LLIDSource: uint32(src), FullMessageFlag: true}))
	tx, err := data.NewSender(&data.SenderConfig{ColorCode: 1, Source: layer2.SyncSourceMS, Slot: enums.Slot2, ResponseWait: time.Second})
	require.NoError(t, err)
	require.NoError(t, tx.Send(0, &data.Message{Header: h, Rate: enums.DataTypeRate12, Payload: packet}))
	var outs []data.Output
	var pkts []models.Packet
	for now := time.Duration(0); now < 3*time.Second && tx.State() != data.SenderWaitResponse; now += 60 * time.Millisecond {
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

func TestDataToTheLocationServerIsAnsweredOnItsRepeater(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)

	for _, p := range confirmedReport(t, 1000002, 5057, 100003) {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
	select {
	case rp := <-srv.Packets:
		assert.Equal(t, uint(100003), rp.RepeaterID, "the answer goes out where the report came in")
		assert.Equal(t, uint(5057), rp.Packet.Src)
		assert.Equal(t, uint(1000002), rp.Packet.Dst)
		assert.False(t, rp.Packet.GroupCall)
	case <-time.After(3 * time.Second):
		t.Fatal("the location server did not answer a confirmed report")
	}
	expectNone(t, srv, "only the answer goes out")
}
