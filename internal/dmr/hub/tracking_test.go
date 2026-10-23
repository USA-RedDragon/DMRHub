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
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
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
