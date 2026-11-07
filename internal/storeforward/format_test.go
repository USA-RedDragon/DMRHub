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

package storeforward_test

import (
	"bufio"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/storeforward"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sender    = 3109998
	recipient = 3109999
)

type airText struct {
	msg  data.Message
	text string
}

func airTexts(t *testing.T, name string) []airText {
	t.Helper()
	f, err := os.Open("../dmr/testdata/" + name + ".txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Lost: -1})
	require.NoError(t, err)
	var out []airText
	var events []data.Event
	var now time.Duration
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) != 2 || strings.HasPrefix(fs[0], "#") {
			continue
		}
		ms, err := strconv.ParseFloat(fs[0], 64)
		require.NoError(t, err)
		b, err := hex.DecodeString(fs[1])
		require.NoError(t, err)
		require.Len(t, b, len(layer2.RawBurst{}))
		raw := layer2.RawBurst(b)
		now = time.Duration(ms * float64(time.Millisecond))
		events = rx.Burst(now, &raw, layer2.Hint{}, events[:0])
		for _, ev := range events {
			if ev.Kind != data.EventDelivered {
				continue
			}
			body, text, err := sms.Decode(nil, &ev.Message.Header, ev.Message.Payload)
			if err != nil || text.Group {
				continue
			}
			m := ev.Message
			m.Payload = append([]byte(nil), m.Payload...)
			out = append(out, airText{msg: m, text: string(body)})
		}
	}
	require.NoError(t, sc.Err())
	return out
}

func stored(t *testing.T, m data.Message) *models.Message {
	t.Helper()
	h, err := m.Header.Encode()
	require.NoError(t, err)
	dst, src, ok := m.Header.Addresses()
	require.True(t, ok)
	return &models.Message{
		SourceID: uint(src), DestinationID: uint(dst), ChecksumOK: true, Rate: uint8(m.Rate),
		Proprietary: m.Proprietary != nil, Raw: append(h[:], m.Payload...),
	}
}

func onAir(t *testing.T, m data.Message) data.Message {
	t.Helper()
	tx, err := data.NewSender(&data.SenderConfig{Access: enums.AccessImpolite, Slot: enums.Slot1, Retries: 1})
	require.NoError(t, err)
	rx, err := data.NewReceiver(&data.ReceiverConfig{Lost: -1})
	require.NoError(t, err)
	require.NoError(t, tx.Send(0, &m))
	var outs []data.Output
	var events []data.Event
	for now := time.Duration(0); now < 30*time.Second; now += 10 * time.Millisecond {
		outs = tx.Tick(now, outs[:0])
		for i := range outs {
			if outs[i].Kind != data.OutputBurst {
				continue
			}
			events = rx.Burst(now, &outs[i].Burst, layer2.Hint{}, events[:0])
			for _, ev := range events {
				if ev.Kind == data.EventDelivered {
					return ev.Message
				}
			}
		}
	}
	t.Fatal("the sender sent no whole message")
	return data.Message{}
}

func TestFormatOfAirTextsGivesTheRadiosOwnOctets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		capture   string
		texts     int
		format    string
		confirmed bool
		rate      enums.DataType
	}{
		{capture: "anytone-dmr-text", texts: 1, format: "AnyTone", rate: enums.DataTypeRate12},
		{capture: "anytone-sms-xpr", texts: 4, format: "AnyTone", confirmed: true, rate: enums.DataTypeRate34},
		{capture: "anytone-v105-phaseA", texts: 3, format: "AnyTone", rate: enums.DataTypeRate12},
	}
	for _, tc := range cases {
		t.Run(tc.capture, func(t *testing.T) {
			t.Parallel()
			texts := airTexts(t, tc.capture)
			require.Len(t, texts, tc.texts)
			for _, at := range texts {
				tpl := stored(t, at.msg)
				f, err := storeforward.FormatOf(tpl)
				require.NoError(t, err)
				assert.Equal(t, tc.format, f.Name())
				assert.Equal(t, tc.confirmed, f.Confirmed())

				own, err := f.Build(at.text, tpl.SourceID, tpl.DestinationID)
				require.NoError(t, err)
				got := onAir(t, own)
				assert.Equal(t, at.msg.Payload, got.Payload, "the user data of the radio's own text")
				want, err := at.msg.Header.Encode()
				require.NoError(t, err)
				gotH, err := got.Header.Encode()
				require.NoError(t, err)
				assert.Equal(t, hex.EncodeToString(want[:]), hex.EncodeToString(gotH[:]), "the data header of the radio's own text")
				assert.Equal(t, tc.rate, got.Rate)

				held, err := f.Build("Net starts at 8, see you there", sender, recipient)
				require.NoError(t, err)
				got = onAir(t, held)
				body, text, err := sms.Decode(nil, &got.Header, got.Payload)
				require.NoError(t, err)
				assert.Equal(t, "Net starts at 8, see you there", string(body))
				assert.Equal(t, tc.format, text.Format.String())
				assert.Equal(t, tc.confirmed, text.Confirmed)
				assert.Equal(t, uint32(sender), text.Source)
				assert.Equal(t, uint32(recipient), text.Destination)
				assert.False(t, text.Group)
				assert.Equal(t, tc.rate, got.Rate)
				assert.Equal(t, endsInNUL(t, at.msg), endsInNUL(t, got), "a NUL after the text when the radio sends one")
			}
		})
	}
}

func endsInNUL(t *testing.T, m data.Message) bool {
	t.Helper()
	p := m.Payload
	if sap, ok := m.Header.SAP(); ok && sap == enums.SAPIPBasedPacketData {
		d, err := ip.Decode(sap, p)
		require.NoError(t, err)
		p = d.Payload
	}
	return len(p) >= 2 && p[len(p)-2] == 0 && p[len(p)-1] == 0
}

// TestFormatOfRefusesTheHSMSTextOfTheAirCapture reads the H-SMS text of
// a DA-7X2, whose first two octets are 45 00. The meaning of those octets
// is not known, so the hub cannot build another text that the radio would
// send, and must not send one.
func TestFormatOfRefusesTheHSMSTextOfTheAirCapture(t *testing.T) {
	t.Parallel()
	texts := airTexts(t, "anytone-hsms")
	require.Len(t, texts, 1)
	_, err := storeforward.FormatOf(stored(t, texts[0].msg))
	assert.ErrorIs(t, err, storeforward.ErrNotReproducible)
}

func synthetic(t *testing.T, text *sms.Text, rate enums.DataType) *models.Message {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, rate)
	require.NoError(t, err)
	return stored(t, data.Message{Header: p.Header, Rate: rate, Payload: payload})
}

func TestFormatOfRefusesWhatItCannotBuildExactly(t *testing.T) {
	t.Parallel()
	at := airTexts(t, "anytone-dmr-text")[0]
	base := stored(t, at.msg)

	noRate := *base
	noRate.Rate = 0
	pHead := *base
	pHead.Proprietary = true
	group := *base
	group.IsGroup = true
	badCRC := *base
	badCRC.BadCRC = true
	ttl := *base
	ttl.Raw = append([]byte(nil), base.Raw...)
	ttl.Raw[12+1] = 0x10
	ttl.Raw[12+10], ttl.Raw[12+11] = 0, 0
	sum := uint32(0)
	for i := 12; i < 12+20; i += 2 {
		sum += uint32(ttl.Raw[i])<<8 | uint32(ttl.Raw[i+1])
	}
	for sum > 0xffff {
		sum = sum&0xffff + sum>>16
	}
	ttl.Raw[12+10], ttl.Raw[12+11] = byte(^sum>>8), byte(^sum)
	for name, tc := range map[string]struct {
		m    *models.Message
		want error
	}{
		"stored without its rate": {m: &noRate, want: storeforward.ErrNoFormat},
		"with a P_HEAD":           {m: &pHead, want: storeforward.ErrNoFormat},
		"to a talkgroup":          {m: &group, want: storeforward.ErrNoFormat},
		"with a bad CRC":          {m: &badCRC, want: storeforward.ErrNoFormat},
		"with a type of service":  {m: &ttl, want: storeforward.ErrNotReproducible},
	} {
		_, err := storeforward.FormatOf(tc.m)
		assert.True(t, errors.Is(err, tc.want), "%s: %v", name, err)
	}
}

func TestFormatBuildsATMSTextLikeTheTemplate(t *testing.T) {
	t.Parallel()
	tpl := synthetic(t, &sms.Text{Format: sms.FormatTMS, Source: recipient, Destination: sender, Sequence: 5, AckRequired: true, Body: []byte("MOTO TEST")}, enums.DataTypeRate34)
	f, err := storeforward.FormatOf(tpl)
	require.NoError(t, err)
	m, err := f.Build("QSL", sender, recipient)
	require.NoError(t, err)
	body, text, err := sms.Decode(nil, &m.Header, m.Payload)
	require.NoError(t, err)
	assert.Equal(t, "QSL", string(body))
	assert.Equal(t, sms.FormatTMS, text.Format)
	assert.True(t, text.AckRequired)
	assert.Equal(t, enums.DataTypeRate34, m.Rate)
	_, isU := m.Header.AsUnconfirmed()
	assert.True(t, isU)
	assert.Equal(t, pdu.FormatUnconfirmed, m.Header.Format())
}

func TestFormatBuildsTheTextOfEveryFormatThatDMRgoBuilds(t *testing.T) {
	t.Parallel()
	for _, tc := range []sms.Text{
		{Format: sms.FormatTMS, Body: []byte("tms")},
		{Format: sms.FormatTMS, Confirmed: true, Body: []byte("tms confirmed")},
		{Format: sms.FormatIP, Body: []byte("ip")},
		{Format: sms.FormatIP, Body: []byte("ip with a nul\x00")},
		{Format: sms.FormatAnyTone, Confirmed: true, Body: []byte("anytone")},
		{Format: sms.FormatDefinedData, DD: enums.DDISO8859Part1, Body: []byte("latin 1")},
		{Format: sms.FormatDefinedData, DD: enums.DDUTF16, Confirmed: true, Body: []byte("utf 16")},
		{Format: sms.FormatHytera, DD: enums.DDBCD, Body: []byte("hytera")},
	} {
		tc.Source, tc.Destination = recipient, sender
		for _, rate := range []enums.DataType{enums.DataTypeRate12, enums.DataTypeRate34, enums.DataTypeRate1} {
			f, err := storeforward.FormatOf(synthetic(t, &tc, rate))
			require.NoError(t, err, "%s %s", tc.Format, tc.Body)
			m, err := f.Build("back to you", sender, recipient)
			require.NoError(t, err)
			got := onAir(t, m)
			body, text, err := sms.Decode(nil, &got.Header, got.Payload)
			require.NoError(t, err)
			assert.Equal(t, "back to you", string(body))
			assert.Equal(t, tc.Format, text.Format)
			assert.Equal(t, tc.Confirmed, text.Confirmed)
			assert.Equal(t, tc.DD, text.DD)
			assert.Equal(t, rate, got.Rate)
		}
	}
}

func TestFormatRefusesATextTheFormatCannotCarry(t *testing.T) {
	t.Parallel()
	f, err := storeforward.FormatOf(synthetic(t, &sms.Text{Format: sms.FormatDefinedData, DD: enums.DD7Bit, Source: recipient, Destination: sender, Body: []byte("seven bit")}, enums.DataTypeRate12))
	require.NoError(t, err)
	_, err = f.Build("中文", sender, recipient)
	assert.ErrorIs(t, err, storeforward.ErrText)
}
