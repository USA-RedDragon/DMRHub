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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/plugins/locator"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sendConfirmedText sends text as the bursts of a data call from its
// source through c, on timeslot 2.
func sendConfirmedText(t *testing.T, c *testutils.MMDVMClient, text *sms.Text, stream uint) {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	for i, b := range bursts {
		require.NoError(t, c.SendDMRD(models.Packet{
			Signature: string(dmrconst.CommandDMRD), Seq: uint(i), Src: uint(text.Source), Dst: uint(text.Destination), Slot: true,
			FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(b.SlotType().DataType), StreamID: stream, DMRData: b, BER: -1, RSSI: -1,
		}))
	}
}

// responsesTo returns the responses of the data link layer that the radio
// heard in pkts.
func responsesTo(t *testing.T, radio uint, pkts []models.Packet) []packet.Response {
	t.Helper()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(radio), Source: layer2.SyncSourceMS, Lost: -1}) //nolint:gosec // a test ID
	require.NoError(t, err)
	var out []packet.Response
	var events []data.Event
	for i, p := range pkts {
		if p.Dst != radio || p.FrameType != dmrconst.FrameDataSync {
			continue
		}
		at := time.Duration(i) * 60 * time.Millisecond
		raw := layer2.RawBurst(p.DMRData)
		d := p.Data()
		events = rx.Burst(at, &raw, d.Hint(at), events[:0])
		for _, e := range events {
			if e.Kind == data.EventResponse {
				out = append(out, e.Response)
			}
		}
	}
	return out
}

// TestATextToTheLocatorGetsOneACK sends a confirmed text to an ID of the
// location server, which answers its own confirmed data, and checks that
// the radio gets exactly one response, from the hub.
func TestATextToTheLocatorGetsOneACK(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const radio = uint(1000001)
		stack.SeedUser(t, radio, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, radio, testPassword)
		stack.StartServers(t)
		stack.RunPlugin(t, "Location server", []models.PluginRange{{First: 5050, Last: 5059}}, locator.Serve)

		c := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		defer c.Close()
		require.NoError(t, c.WaitReady(handshakeWait))

		sendConfirmedText(t, c, &sms.Text{Format: sms.FormatIP, Source: uint32(radio), Destination: 5050, Confirmed: true, Body: []byte("STATUS")}, 8301)
		got := testutils.Collect(c.ReceivedPackets(), deliverWait, func(p []models.Packet) bool { return len(responsesTo(t, radio, p)) > 0 })
		got = append(got, c.Drain(2*time.Second)...)
		assert.Equal(t, []packet.Response{packet.ResponseACK}, responsesTo(t, radio, got), "one ACK")
	})
}
