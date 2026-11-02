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
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sfNet struct {
	stack *testutils.IntegrationStack
	radio *testutils.MMDVMClient
	other *testutils.MMDVMClient
}

func newSFNet(t *testing.T, stack *testutils.IntegrationStack) *sfNet {
	t.Helper()
	stack.SeedUser(t, anyTone, "N6CALL")
	stack.SeedUser(t, otherRadio, "KF5XYZ")
	stack.SeedMMDVMRepeater(t, anyToneRpt, anyTone, testPassword)
	stack.SeedMMDVMRepeater(t, otherRpt, otherRadio, testPassword)
	stack.StartServers(t)
	require.NoError(t, models.SetStoreForward(stack.DB, anyTone, true, time.Now()))
	n := &sfNet{stack: stack}
	for _, c := range []**testutils.MMDVMClient{&n.radio, &n.other} {
		id := uint32(anyToneRpt)
		if c == &n.other {
			id = otherRpt
		}
		*c = testutils.NewMMDVMClient(id, "", testPassword)
		t.Cleanup((*c).Close)
		require.NoError(t, (*c).Connect(stack.MMDVMAddr))
		require.NoError(t, (*c).WaitReady(handshakeWait))
	}
	return n
}

// textToRadio sends a TMS text from the other radio to the AnyTone, which
// the hub has not heard, and returns once the hub held it.
func (n *sfNet) textToRadio(t *testing.T, body string, stream uint) {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatTMS, Source: otherRadio, Destination: anyTone, Body: []byte(body)}, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 7, layer2.SyncSourceMS)
	require.NoError(t, err)
	pkts := make([]models.Packet, 0, len(bursts))
	for i, raw := range bursts {
		pkts = append(pkts, dataPacket(call{src: otherRadio, dst: anyTone}, true, stream, i, raw))
	}
	sendPackets(t, n.other.SendDMRD, pkts)
	waitFor(t, "the text to be held", func() bool {
		var count int64
		require.NoError(t, n.stack.DB.Model(&models.HeldMessage{}).Count(&count).Error)
		return count > 0
	})
}

func (n *sfNet) settle() {
	n.stack.CallTracker.Flush()
	n.stack.MessageStore.Flush()
	n.stack.StoreForward.Flush()
}

func (n *sfNet) heard(t *testing.T, radio uint) {
	t.Helper()
	waitFor(t, fmt.Sprintf("the record of where radio %d was heard", radio), func() bool {
		_, err := n.stack.KV.Get(context.Background(), fmt.Sprintf("dmrhub:lastheard:%d", radio))
		return err == nil
	})
	n.settle()
}

func (n *sfNet) held(t *testing.T) []models.HeldMessage {
	t.Helper()
	var rows []models.HeldMessage
	require.NoError(t, n.stack.DB.Order("id").Find(&rows).Error)
	return rows
}

// textsAt decodes the data messages in the bursts that a repeater got.
func textsAt(t *testing.T, pkts []models.Packet) []data.Message {
	t.Helper()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Lost: -1})
	require.NoError(t, err)
	var out []data.Message
	var events []data.Event
	for i, p := range pkts {
		raw := layer2.RawBurst(p.DMRData)
		events = rx.Burst(time.Duration(i)*60*time.Millisecond, &raw, layer2.Hint{}, events[:0])
		for _, ev := range events {
			if ev.Kind == data.EventDelivered {
				m := ev.Message
				m.Payload = append([]byte(nil), m.Payload...)
				out = append(out, m)
			}
		}
	}
	return out
}

// TestStoreAndForwardSendsAHeldTextInTheFormatOfTheRadio sends a TMS text
// to an AnyTone that the hub has not heard. The hub holds it. When the
// AnyTone then sends its own DMR Standard text (the air capture), the hub
// sends the held text to its hotspot in that format, from the sender.
func TestStoreAndForwardSendsAHeldTextInTheFormatOfTheRadio(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newSFNet(t, stack)
		n.textToRadio(t, "Net starts at 8, see you there", 0xB0001)
		rows := n.held(t)
		require.Len(t, rows, 1)
		assert.Equal(t, models.HeldStateHeld, rows[0].State)

		var own []models.Packet
		for i, tx := range splitTransmissions(t, readAirCapture(t, "testdata/anytone-dmr-text.txt"), true) {
			if uint(tx.src) == anyTone {
				own = append(own, tx.packets(0xB0002+uint(i))...)
			}
		}
		require.NotEmpty(t, own)
		sendPackets(t, n.radio.SendDMRD, own)
		n.heard(t, anyTone)

		var got []data.Message
		drainUntil(func(out [][]models.Packet) bool {
			var mine []models.Packet
			for _, p := range out[0] {
				if p.Src == otherRadio && p.Dst == anyTone {
					mine = append(mine, p)
				}
			}
			got = textsAt(t, mine)
			return len(got) > 0
		}, n.radio)
		require.Len(t, got, 1)
		body, text, err := sms.Decode(nil, &got[0].Header, got[0].Payload)
		require.NoError(t, err)
		assert.Equal(t, "Net starts at 8, see you there", string(body))
		assert.Equal(t, sms.FormatAnyTone, text.Format, "the format of the radio, not the TMS of the sender")
		assert.Equal(t, uint32(otherRadio), text.Source)
		assert.Equal(t, uint32(anyTone), text.Destination)
		assert.False(t, text.Confirmed)
		assert.Equal(t, enums.DataTypeRate12, got[0].Rate)

		rows = n.held(t)
		require.Len(t, rows, 1)
		assert.Equal(t, models.HeldStateDelivered, rows[0].State)
	})
}

// TestStoreAndForwardHoldsATextForARadioThatSentNoText holds a text for
// an AnyTone that the hub then hears only in a voice call. The hub does
// not know the text format of the radio, so it keeps the text held and
// sends the radio nothing.
func TestStoreAndForwardHoldsATextForARadioThatSentNoText(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newSFNet(t, stack)
		stack.SeedTalkgroup(t, goldenTG, "TG9")
		n.textToRadio(t, "anyone home", 0xC0001)
		require.Len(t, n.held(t), 1)

		require.NoError(t, n.radio.SendDMRD(makeGroupVoicePacket(anyTone, goldenTG, 0xC0002, true)))
		require.NoError(t, n.radio.SendDMRD(makeGroupVoiceTermPacket(anyTone, goldenTG, 0xC0002, true)))
		n.heard(t, anyTone)

		rows := n.held(t)
		require.Len(t, rows, 1)
		assert.Equal(t, models.HeldStateHeld, rows[0].State)
		assert.Equal(t, uint(0), rows[0].Attempts)
		for _, p := range n.radio.Drain(0) {
			assert.False(t, p.Src == otherRadio && p.Dst == anyTone, "a burst of the held text reached the radio")
		}
	})
}

// TestStoreAndForwardWaitsForTheACKOfARadioThatAsksForOne holds a text for
// an AnyTone whose own texts ask for a response (the confirmed rate 3/4
// air capture). The hub sends the held text confirmed, and the hotspot of
// the radio answers with the ACK that a radio would send; the hub then
// marks the text delivered with the ACK.
func TestStoreAndForwardWaitsForTheACKOfARadioThatAsksForOne(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newSFNet(t, stack)
		n.textToRadio(t, "QSL, thanks for the contact", 0xD0001)

		var own []models.Packet
		for i, tx := range splitTransmissions(t, readAirCapture(t, "testdata/anytone-sms-xpr.txt"), true) {
			if uint(tx.src) == anyTone {
				own = append(own, tx.packets(0xD0002+uint(i))...)
			}
		}
		require.NotEmpty(t, own)
		sendPackets(t, n.radio.SendDMRD, own)

		rx, err := data.NewReceiver(&data.ReceiverConfig{Respond: true, Source: layer2.SyncSourceMS, Lost: -1})
		require.NoError(t, err)
		var at time.Duration
		var confirmed bool
		stream := uint(0xD1000)
		waitFor(t, "the held text to be delivered with an ACK", func() bool {
			var answer []layer2.RawBurst
			for _, p := range n.radio.Drain(100 * time.Millisecond) {
				if p.Src != otherRadio || p.Dst != anyTone {
					continue
				}
				raw := layer2.RawBurst(p.DMRData)
				at += 60 * time.Millisecond
				events := rx.Burst(at, &raw, layer2.Hint{}, nil)
				for _, ev := range events {
					switch ev.Kind {
					case data.EventDelivered:
						_, text, err := sms.Decode(nil, &ev.Message.Header, ev.Message.Payload)
						confirmed = err == nil && text.Format == sms.FormatAnyTone && text.Confirmed
					case data.EventRespond:
						answer = append(answer, ev.Burst)
					case data.EventCallStart, data.EventResponse, data.EventCallEnd, data.EventReassembled:
					}
				}
			}
			if len(answer) > 0 {
				stream++
				pkts := make([]models.Packet, 0, len(answer))
				for i, raw := range answer {
					pkts = append(pkts, dataPacket(call{src: anyTone, dst: otherRadio}, true, stream, i, raw))
				}
				sendPackets(t, n.radio.SendDMRD, pkts)
			}
			rows := n.held(t)
			return len(rows) == 1 && rows[0].State == models.HeldStateDelivered
		})
		assert.True(t, confirmed, "the hub sent the held text confirmed, as the radio sends its own")
		rows := n.held(t)
		assert.Equal(t, models.CallResponseACK, rows[0].Response)
	})
}
