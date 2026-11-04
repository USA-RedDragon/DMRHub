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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/plugins/status"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/USA-RedDragon/dmrgo/v2/trunk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	statusRadio    = uint(1000001)
	statusRepeater = 100001
)

// statusSlot2 returns what the status says of timeslot 2 of
// statusRepeater.
func statusSlot2() *pluginv1.SlotInfo {
	return &pluginv1.SlotInfo{Slot: 2, StaticTalkgroups: []uint32{3100}}
}

// statusStack runs the status plugin at 5000 on stack, with statusRepeater
// of statusRadio's owner linked to talkgroup 3100 on timeslot 2, and
// returns a connected client of statusRepeater.
func statusStack(t *testing.T, stack *testutils.IntegrationStack) (*testutils.MMDVMClient, models.Plugin) {
	t.Helper()
	stack.SeedUser(t, statusRadio, "N0CALL")
	stack.SeedTalkgroup(t, 3100, "TG3100")
	stack.SeedMMDVMRepeater(t, statusRepeater, statusRadio, testPassword)
	stack.AssignTS2StaticTG(t, statusRepeater, 3100)
	stack.StartServers(t)
	p := stack.Plugins(t).Run(t, "Status", []models.PluginRange{{First: status.ID, Last: status.ID}}, status.Serve)
	c := testutils.NewMMDVMClient(statusRepeater, "", testPassword)
	require.NoError(t, c.Connect(stack.MMDVMAddr))
	t.Cleanup(c.Close)
	require.NoError(t, c.WaitReady(handshakeWait))
	return c, p
}

func callStatus(t *testing.T, c *testutils.MMDVMClient, stream uint) {
	t.Helper()
	pkts := make([]models.Packet, 0, 6)
	pkts = append(pkts, makePrivateVoiceHeaderPacket(status.ID, stream))
	for i := range uint(4) {
		v := makePrivateVoicePacket(status.ID, stream)
		v.Seq, v.DTypeOrVSeq = i+1, i%6
		pkts = append(pkts, v)
	}
	term := makePrivateVoiceTermPacket(status.ID, stream)
	term.Seq = 5
	pkts = append(pkts, term)
	for _, p := range pkts {
		p.Slot = true
		require.NoError(t, c.SendDMRD(p))
	}
}

// TestACallTo5000GetsTheSpokenStatusOfItsTimeslot calls 5000 on timeslot 2
// and checks that the caller hears, a second after the call, a private
// call from 5000 with the clips of the status: "Timeslot two. Linked:
// talkgroup three one zero zero, static. Dynamic: none."
func TestACallTo5000GetsTheSpokenStatusOfItsTimeslot(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		c, _ := statusStack(t, stack)
		clips := status.Clips(statusSlot2())
		require.Equal(t, []string{"word-timeslot", "digit-2", "word-linked", "word-talkgroup", "digit-3", "digit-1", "digit-0", "digit-0", "word-static", "word-dynamic", "word-none"}, clips)

		callStatus(t, c, 8401)
		got := receiveCall(c, statusRadio)
		require.NotEmpty(t, got, "no status call")
		var slots [][9]byte
		for _, p := range got {
			if p.Dst != statusRadio {
				continue
			}
			assert.Equal(t, uint(status.ID), p.Src)
			assert.False(t, p.GroupCall)
			assert.True(t, p.Slot, "on the timeslot of the call")
			if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
				s := layer2.RawBurst(p.DMRData).VoiceSlots()
				slots = append(slots, s[:]...)
			}
		}
		var want [][9]byte
		for _, clip := range clips {
			f, err := prompts.Frames(prompts.DefaultVoice, clip, transcode.CodecAMBE)
			require.NoError(t, err)
			want = append(want, f...)
		}
		require.GreaterOrEqual(t, len(slots), len(want))
		assert.Equal(t, want, slots[:len(want)], "the clips of the status, in order")
	})
}

// sentData returns the packets from 5000 to the radio in pkts, as the
// radio's data receiver delivers them.
func sentData(t *testing.T, pkts []models.Packet) []data.Message {
	t.Helper()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(statusRadio), Source: layer2.SyncSourceMS, Lost: -1})
	require.NoError(t, err)
	var out []data.Message
	var events []data.Event
	for i, p := range pkts {
		if p.Dst != statusRadio || p.Src != status.ID || p.FrameType != dmrconst.FrameDataSync {
			continue
		}
		at := time.Duration(i) * 60 * time.Millisecond
		raw := layer2.RawBurst(p.DMRData)
		events = rx.Burst(at, &raw, layer2.Hint{Kind: layer2.KindData}, events[:0])
		for _, e := range events {
			if e.Kind == data.EventDelivered {
				m := e.Message
				m.Payload = append([]byte(nil), m.Payload...)
				out = append(out, m)
			}
		}
	}
	return out
}

func sendDataBursts(t *testing.T, c *testutils.MMDVMClient, src, dst, stream uint, bursts []layer2.RawBurst) {
	t.Helper()
	for i, b := range bursts {
		require.NoError(t, c.SendDMRD(models.Packet{
			Signature: string(dmrconst.CommandDMRD), Seq: uint(i), Src: src, Dst: dst, Slot: true,
			FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(b.SlotType().DataType), StreamID: stream, DMRData: b, BER: -1, RSSI: -1,
		}))
	}
}

// TestATextTo5000GetsTheStatusInTheFormatOfTheText sends a text to 5000
// in each text format and checks that every packet that 5000 sends back is
// in the format, header type and rate of that text, and that one of them
// is the written status.
func TestATextTo5000GetsTheStatusInTheFormatOfTheText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		text sms.Text
		rate enums.DataType
	}{
		{"tms", sms.Text{Format: sms.FormatTMS, AckRequired: true, Sequence: 4}, enums.DataTypeRate34},
		{"ip", sms.Text{Format: sms.FormatIP}, enums.DataTypeRate12},
		{"anytone", sms.Text{Format: sms.FormatAnyTone}, enums.DataTypeRate12},
		{"defined_data", sms.Text{Format: sms.FormatDefinedData, DD: enums.DDISO8859Part1}, enums.DataTypeRate1},
		{"hytera", sms.Text{Format: sms.FormatHytera, DD: enums.DDBCD}, enums.DataTypeRate12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
				c, _ := statusStack(t, stack)
				text := tc.text
				text.Source, text.Destination, text.Body = uint32(statusRadio), status.ID, []byte("STATUS")
				var h sms.Header
				payload, err := sms.AppendPacket(nil, &text, &h)
				require.NoError(t, err)
				p, err := packet.NewPacket(h.Data, payload, tc.rate)
				require.NoError(t, err)
				bursts, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
				require.NoError(t, err)
				sendDataBursts(t, c, statusRadio, status.ID, 8501, bursts)

				words := status.Words(statusSlot2())
				isReply := func(m data.Message) bool {
					body, _, err := sms.Decode(nil, &m.Header, m.Payload)
					return err == nil && string(body) == words
				}
				got := testutils.Collect(c.ReceivedPackets(), deliverWait, func(pkts []models.Packet) bool {
					for _, m := range sentData(t, pkts) {
						if isReply(m) {
							return true
						}
					}
					return false
				})
				got = append(got, c.Drain(2*time.Second)...)
				msgs := sentData(t, got)
				require.NotEmpty(t, msgs)
				replies := 0
				for _, m := range msgs {
					assert.Equal(t, p.Header.Format(), m.Header.Format(), "only the header type of the text")
					assert.Equal(t, tc.rate, m.Rate, "only the rate of the text")
					_, decoded, err := sms.Decode(nil, &m.Header, m.Payload)
					if err != nil {
						assert.Equal(t, sms.FormatTMS, tc.text.Format, "only a TMS acknowledgement is not a text: %v", err)
						continue
					}
					assert.Equal(t, tc.text.Format, decoded.Format, "only the encoding of the text")
					assert.Equal(t, tc.text.DD, decoded.DD)
					if isReply(m) {
						replies++
					}
				}
				assert.Equal(t, 1, replies, "one written status")
			})
		})
	}
}

func udtStatusBursts(t *testing.T) []layer2.RawBurst {
	t.Helper()
	var a trunk.Appended
	require.NoError(t, a.SetText(enums.UDTISO8, []byte("STATUS")))
	var h pdu.DataHeader
	require.NoError(t, h.SetUDT(pdu.UDTHeader{
		SAP: enums.SAPUnifiedDataTransport, UDTFormat: enums.UDTISO8, LLIDDestination: status.ID, LLIDSource: uint32(statusRadio),
		PadNibble: a.PadNibble, UAB: uint8(a.Blocks - 1), Opcode: pdu.UDTOpcodeInboundHeader, //nolint:gosec // 1 to 4 blocks
	}))
	head, err := h.Encode()
	require.NoError(t, err)
	raw, err := layer2.BuildDataBurst(head, enums.DataTypeDataHeader, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	blocks := make([][12]byte, a.Blocks)
	out := make([]layer2.RawBurst, 0, 1+len(blocks))
	out = append(out, raw)
	for i := range blocks {
		copy(blocks[i][:], a.Data[12*i:min(12*i+12, trunk.MaxAppendedOctets)])
	}
	pdu.SetMBCLastBlockCRC(blocks)
	for _, b := range blocks {
		raw, err := layer2.BuildDataBurst(b, enums.DataTypeRate12, 1, layer2.SyncSourceMS)
		require.NoError(t, err)
		out = append(out, raw)
	}
	return out
}

// TestAUDTTo5000GetsNoTextInAnotherFormat sends a UDT text to 5000, which
// the hub cannot answer in its own format yet, and checks that 5000 sends
// the radio nothing.
func TestAUDTTo5000GetsNoTextInAnotherFormat(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		c, _ := statusStack(t, stack)
		sendDataBursts(t, c, statusRadio, status.ID, 8601, udtStatusBursts(t))
		for _, p := range c.Drain(3 * time.Second) {
			assert.NotEqual(t, uint(status.ID), p.Src, "nothing from 5000 to a UDT: %+v", p)
		}
		stack.CallTracker.Flush()
		stack.MessageStore.Flush()
		var n int64
		require.NoError(t, stack.DB.Model(&models.Message{}).Where("format = ? AND destination_id = ?", models.MessageFormatUDT, status.ID).Count(&n).Error)
		assert.Equal(t, int64(1), n, "the hub kept the UDT as a text")
	})
}

// TestADisabledStatusAnswersNothing turns the status plugin off and checks
// that neither a call nor a text to 5000 gets an answer.
func TestADisabledStatusAnswersNothing(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		c, p := statusStack(t, stack)
		require.NoError(t, stack.DB.Model(&models.Plugin{}).Where("id = ?", p.ID).Update("enabled", false).Error)
		require.NoError(t, plugins.PublishChanged(stack.PubSub))
		require.NoError(t, stack.Plugins(t).Server.Registry().Reload())
		_, owned := stack.Plugins(t).Server.Registry().Owner(status.ID)
		require.False(t, owned)

		callStatus(t, c, 8701)
		text := &sms.Text{Format: sms.FormatTMS, AckRequired: true, Confirmed: true, Source: uint32(statusRadio), Destination: status.ID, Body: []byte("STATUS")}
		var h sms.Header
		payload, err := sms.AppendPacket(nil, text, &h)
		require.NoError(t, err)
		pk, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
		require.NoError(t, err)
		bursts, err := packet.Bursts(&pk, 1, layer2.SyncSourceMS)
		require.NoError(t, err)
		sendDataBursts(t, c, statusRadio, status.ID, 8702, bursts)
		for _, got := range c.Drain(4 * time.Second) {
			assert.NotEqual(t, uint(status.ID), got.Src, "no answer from a plugin that is off: %+v", got)
		}
	})
}
