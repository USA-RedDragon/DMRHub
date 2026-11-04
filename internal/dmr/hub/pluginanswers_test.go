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
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	answerRadio    = uint(1000001)
	answerRepeater = uint(100004)
	answerService  = uint(9990)
)

type offlinePlugins struct{ *fakePlugins }

func (offlinePlugins) Online(uint) bool { return false }

func textPacket(t *testing.T, text *sms.Text, rate enums.DataType) packet.Packet {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, rate)
	require.NoError(t, err)
	return p
}

// routeBursts routes bursts from the radio to the plugin's ID, one slot
// apart by wait, the time.Sleep of the bubble of the test.
func routeBursts(t *testing.T, h *hub.Hub, sourceName string, stream uint, bursts []layer2.RawBurst, wait func(time.Duration)) {
	t.Helper()
	for i, b := range bursts {
		h.RoutePacket(context.Background(), models.Packet{
			Signature: string(dmrconst.CommandDMRD), Seq: uint(i), Src: answerRadio, Dst: answerService, Repeater: answerRepeater, Slot: true,
			FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(b.SlotType().DataType), StreamID: stream, DMRData: b, BER: -1, RSSI: -1,
		}, sourceName)
		wait(60 * time.Millisecond)
	}
}

func burstsOf(t *testing.T, p *packet.Packet) []layer2.RawBurst {
	t.Helper()
	bursts, err := packet.Bursts(p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return bursts
}

// answered reads what the hub sent to the repeater, once the test waited
// for it, as a radio would: the responses it heard and the packets
// delivered to it.
func answered(t *testing.T, srv *hub.ServerHandle) ([]packet.Response, []data.Message) {
	t.Helper()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(answerRadio), Source: layer2.SyncSourceMS, Lost: -1})
	require.NoError(t, err)
	var responses []packet.Response
	var delivered []data.Message
	var events []data.Event
	for i := 0; ; i++ {
		var rp hub.RoutedPacket
		select {
		case rp = <-srv.Packets:
		default:
			return responses, delivered
		}
		assert.Equal(t, answerRepeater, rp.RepeaterID)
		p := rp.Packet
		assert.Equal(t, answerService, p.Src)
		assert.Equal(t, answerRadio, p.Dst)
		assert.True(t, p.Slot, "the answer goes on the timeslot of the text")
		at := time.Duration(i) * 60 * time.Millisecond
		raw := layer2.RawBurst(p.DMRData)
		events = rx.Burst(at, &raw, layer2.Hint{Kind: layer2.KindData}, events[:0])
		for _, e := range events {
			switch e.Kind {
			case data.EventResponse:
				responses = append(responses, e.Response)
			case data.EventDelivered:
				m := e.Message
				m.Payload = append([]byte(nil), m.Payload...)
				delivered = append(delivered, m)
			case data.EventCallStart, data.EventRespond, data.EventCallEnd, data.EventReassembled:
			}
		}
	}
}

func tmsText(confirmed bool) *sms.Text {
	return &sms.Text{
		Format: sms.FormatTMS, Source: uint32(answerRadio), Destination: uint32(answerService), Confirmed: confirmed,
		AckRequired: true, Sequence: 5, Body: []byte("STATUS"),
	}
}

func wantTMSAck(t *testing.T, text *sms.Text, rate enums.DataType, got []data.Message) {
	t.Helper()
	require.Len(t, got, 1, "one TMS acknowledgement")
	tp := textPacket(t, text, rate)
	_, decoded, err := sms.Decode(nil, &tp.Header, textPayload(t, text))
	require.NoError(t, err)
	var h sms.Header
	want, err := sms.AppendAck(nil, &decoded, &h)
	require.NoError(t, err)
	assert.Equal(t, want, got[0].Payload)
	assert.Equal(t, rate, got[0].Rate, "in the rate of the text")
	_, ok := got[0].Header.AsUnconfirmed()
	assert.True(t, ok, "a TMS acknowledgement goes in a U_HEAD")
}

func textPayload(t *testing.T, text *sms.Text) []byte {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	return payload
}

func TestTheHubAnswersAConfirmedTMSTextToAPluginAsARadioWould(t *testing.T) {
	t.Parallel()
	for _, source := range []string{models.RepeaterTypeMMDVM, models.RepeaterTypeIPSC, models.RepeaterTypeModem} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h, _, srv := pluginHub(t)
				if source != models.RepeaterTypeMMDVM {
					h.RegisterServer(hub.ServerConfig{Name: source, Role: hub.RoleRepeater})
				}
				text := tmsText(true)
				p := textPacket(t, text, enums.DataTypeRate34)
				routeBursts(t, h, source, 8101, burstsOf(t, &p), time.Sleep)
				time.Sleep(10 * time.Second)
				synctest.Wait()
				responses, delivered := answered(t, srv)
				assert.Equal(t, []packet.Response{packet.ResponseACK}, responses)
				wantTMSAck(t, text, enums.DataTypeRate34, delivered)
			})
		})
	}
}

func TestTheHubAcknowledgesAnUnconfirmedTMSTextToAPluginOnlyInTMS(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		text := tmsText(false)
		p := textPacket(t, text, enums.DataTypeRate12)
		routeBursts(t, h, models.RepeaterTypeMMDVM, 8102, burstsOf(t, &p), time.Sleep)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		responses, delivered := answered(t, srv)
		assert.Empty(t, responses, "an unconfirmed text gets no response")
		wantTMSAck(t, text, enums.DataTypeRate12, delivered)
	})
}

func TestTheHubAnswersAConfirmedETSITextToAPluginWithOnlyAnACK(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		p := textPacket(t, &sms.Text{Format: sms.FormatIP, Source: uint32(answerRadio), Destination: uint32(answerService), Confirmed: true, Body: []byte("STATUS")}, enums.DataTypeRate12)
		routeBursts(t, h, models.RepeaterTypeMMDVM, 8103, burstsOf(t, &p), time.Sleep)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		responses, delivered := answered(t, srv)
		assert.Equal(t, []packet.Response{packet.ResponseACK}, responses)
		assert.Empty(t, delivered, "no TMS acknowledgement for an ETSI text")
	})
}

func TestTheHubAnswersADamagedTextToAPluginWithANACKAndNeverASACK(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		text := tmsText(true)
		text.Body = []byte("a text long enough to fill several blocks of the packet")
		p := textPacket(t, text, enums.DataTypeRate34)
		bursts := burstsOf(t, &p)
		require.Greater(t, len(bursts), 3)
		var err error
		block, ok := p.Blocks[1].(*pdu.Rate34Data)
		require.True(t, ok)
		bad := block.Data
		bad[5] ^= 0xFF
		bursts[2], err = layer2.BuildRate34DataBurst(bad, 1, layer2.SyncSourceMS)
		require.NoError(t, err, "a block whose CRC fails, which a radio answers with a SACK")
		routeBursts(t, h, models.RepeaterTypeMMDVM, 8104, bursts, time.Sleep)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		responses, delivered := answered(t, srv)
		require.NotEmpty(t, responses)
		for _, r := range responses {
			assert.Equal(t, packet.ResponseNACKPacketCRC.Class(), r.Class(), "a NACK, got %s", r)
			assert.NotEqual(t, packet.ResponseSACK, r)
		}
		assert.Empty(t, delivered, "no TMS acknowledgement for a text that did not arrive")
	})
}

func TestTheHubDoesNotAnswerDataToAPluginThatIsNotText(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		var dh pdu.DataHeader
		require.NoError(t, dh.SetConfirmed(pdu.ConfirmedDataHeader{
			SAP: enums.SAPShortData, LLIDDestination: uint32(answerService), LLIDSource: uint32(answerRadio), FullMessageFlag: true,
		}))
		p, err := packet.NewPacket(dh, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, enums.DataTypeRate12)
		require.NoError(t, err)
		routeBursts(t, h, models.RepeaterTypeMMDVM, 8105, burstsOf(t, &p), time.Sleep)
		expectNone(t, srv, "the plugin answers its own data")
	})
}

func TestTheHubDoesNotAnswerForAPluginThatIsOffline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, f, srv := pluginHub(t)
		h.SetPlugins(offlinePlugins{f})
		p := textPacket(t, tmsText(true), enums.DataTypeRate34)
		routeBursts(t, h, models.RepeaterTypeMMDVM, 8106, burstsOf(t, &p), time.Sleep)
		expectNone(t, srv, "no stream, no answer")
	})
}

func TestTheHubDoesNotAnswerATextToAnIDThatNoPluginHas(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		h.SetPlugins(newFakePlugins(map[uint]uint{}))
		p := textPacket(t, tmsText(true), enums.DataTypeRate34)
		routeBursts(t, h, models.RepeaterTypeMMDVM, 8107, burstsOf(t, &p), time.Sleep)
		bubble.None(t, srv.Packets, 2*time.Second, "a disabled plugin gets no answer")
	})
}

func TestTheHubDoesNotAnswerATextFromAPeer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer})
		p := textPacket(t, tmsText(true), enums.DataTypeRate34)
		routeBursts(t, h, "openbridge", 8108, burstsOf(t, &p), time.Sleep)
		expectNone(t, srv, "a peer's text gets its answer from where it came from")
	})
}

func TestTheHubAnswersTheCopyOfATextWhoseACKTheRadioMissed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		text := tmsText(true)
		p := textPacket(t, text, enums.DataTypeRate34)
		routeBursts(t, h, models.RepeaterTypeMMDVM, 8109, burstsOf(t, &p), time.Sleep)
		time.Sleep(time.Second)
		routeBursts(t, h, models.RepeaterTypeMMDVM, 8110, burstsOf(t, &p), time.Sleep)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		responses, delivered := answered(t, srv)
		assert.Equal(t, []packet.Response{packet.ResponseACK, packet.ResponseACK}, responses, "each copy gets its ACK")
		require.Len(t, delivered, 2, "and its TMS acknowledgement")
		wantTMSAck(t, text, enums.DataTypeRate34, delivered[:1])
		assert.Equal(t, delivered[0].Payload, delivered[1].Payload)
	})
}
