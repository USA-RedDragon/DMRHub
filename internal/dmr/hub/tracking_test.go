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
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoutePacketTracksControlBursts(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedUser(t, database, testSrcID, "CALLER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedTalkgroup(t, database, 1, "TG1")

	var c pdu.CSBK
	c.LastBlock = true
	require.NoError(t, c.SetMotorolaCallAlert(pdu.MotorolaCallAlertPDU{TargetAddress: 1000002, SourceAddress: uint32(testSrcID)}))
	raw, err := layer2.BuildBurst(&c, 1, layer2.SyncSourceMS)
	require.NoError(t, err)

	pkt := makeVoicePacket(1000002, 4242, false, false)
	pkt.FrameType = dmrconst.FrameDataSync
	pkt.DTypeOrVSeq = uint(enums.DataTypeCSBK)
	pkt.DMRData = raw
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	var call models.Call
	require.Eventually(t, func() bool {
		return database.Where("active = ?", false).First(&call).Error == nil
	}, 5*time.Second, 50*time.Millisecond)
	assert.Equal(t, models.CallKindCSBK, call.Kind)
	assert.Equal(t, "call_alert", call.SubKind)
	assert.Equal(t, testSrcID, call.SourceID)
	assert.Equal(t, models.RepeaterTypeMMDVM, call.Transport)
}

func TestUndeliverableBurstIsRecordedOnItsTransmission(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedUser(t, database, testSrcID, "CALLER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedTalkgroup(t, database, 1, "TG1")

	var c pdu.CSBK
	c.LastBlock = true
	require.NoError(t, c.SetMotorolaCallAlert(pdu.MotorolaCallAlertPDU{TargetAddress: 1000002, SourceAddress: uint32(testSrcID)}))
	raw, err := layer2.BuildBurst(&c, 1, layer2.SyncSourceMS)
	require.NoError(t, err)

	pkt := makeVoicePacket(1000002, 4243, false, false)
	pkt.FrameType = dmrconst.FrameDataSync
	pkt.DTypeOrVSeq = uint(enums.DataTypeCSBK)
	pkt.DMRData = raw
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)

	egress := pkt
	egress.Repeater = 311304
	require.Eventually(t, func() bool {
		h.ReportUndeliverable(egress, models.RepeaterTypeIPSC, "test")
		var call models.Call
		if database.Where("stream_id = ?", pkt.StreamID).First(&call).Error != nil {
			return false
		}
		return len(call.Undeliverable) == 1 && call.Undeliverable[0] == "ipsc:311304:test"
	}, 5*time.Second, 50*time.Millisecond)
}

func smsBursts(t *testing.T, text *sms.Text) []layer2.RawBurst {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return bursts
}

// TestUndeliverableMarksTheDataPacketOfItsBurst sends two data packets on
// one stream. The routing worker routes the second while the egress of
// the first is still pending, so the undeliverable report of a burst of
// the first comes after the second is tracked. The mark must go on the
// row of the first.
func TestUndeliverableMarksTheDataPacketOfItsBurst(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)

	const stream = 4244
	first := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: 1000001, Destination: 100003, Body: []byte("first")})
	second := smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: 1000001, Destination: 100003, Body: []byte("second")})
	var seq uint
	for _, b := range append(append([]layer2.RawBurst(nil), first...), second...) {
		p := dataHeader(1000001, 100003, 100004, stream)
		seq++
		p.Seq = seq
		p.DTypeOrVSeq = uint(b.SlotType().DataType)
		p.DMRData = b
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
	var routed []models.Packet
	for range len(first) + len(second) {
		select {
		case rp := <-srv.Packets:
			routed = append(routed, rp.Packet)
		case <-time.After(2 * time.Second):
			t.Fatalf("%d of %d bursts routed", len(routed), len(first)+len(second))
		}
	}
	h.ReportUndeliverable(routed[1], models.RepeaterTypeIPSC, "test")

	var calls []models.Call
	require.Eventually(t, func() bool {
		calls = nil
		return database.Where("stream_id = ? AND active = ?", stream, false).Order("id").Find(&calls).Error == nil && len(calls) == 2
	}, 8*time.Second, 50*time.Millisecond)
	assert.Equal(t, "ip", calls[0].DataFormat)
	assert.Equal(t, []string{"ipsc:100003:test"}, calls[0].Undeliverable, "the row of the packet whose burst was undeliverable")
	assert.Empty(t, calls[1].Undeliverable, "the row of the next packet on the stream")
}
