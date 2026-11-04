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

package plugintexts_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/plugintexts"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	radio         = 1000001
	owner         = radio
	service       = 4700
	ownRepeater   = 100001
	otherRepeater = 100002
)

type sent struct {
	repeater uint
	slot     bool
	m        data.Message
}

type fakeSender struct {
	mu     sync.Mutex
	sent   []sent
	result data.Result
}

func (f *fakeSender) SendData(_ context.Context, repeater uint, slot bool, m data.Message) (data.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{repeater, slot, m})
	return f.result, nil
}

func (f *fakeSender) all() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent(nil), f.sent...)
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	require.NoError(t, database.Create(&models.User{ID: owner, Callsign: "N3CALL", Username: "owner", Approved: true}).Error)
	require.NoError(t, database.Create(&models.User{ID: 1000002, Callsign: "N0CALL", Username: "other", Approved: true}).Error)
	require.NoError(t, database.Create(&models.Repeater{ID: ownRepeater, OwnerID: owner, Callsign: "N3CALL", Type: models.RepeaterTypeMMDVM}).Error)
	require.NoError(t, database.Create(&models.Repeater{ID: otherRepeater, OwnerID: 1000002, Callsign: "N0CALL", Type: models.RepeaterTypeMMDVM}).Error)
	return database
}

// radioSent stores a TMS text that the radio sent to the service through
// the repeater on timeslot 2, and radioSentText the text at rate.
func radioSent(t *testing.T, database *gorm.DB, repeater uint) {
	t.Helper()
	radioSentText(t, database, repeater, &sms.Text{Format: sms.FormatTMS, Source: radio, Destination: service, Sequence: 3, Body: []byte("INBOX")}, enums.DataTypeRate34)
}

func radioSentText(t *testing.T, database *gorm.DB, repeater uint, text *sms.Text, rate enums.DataType) {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, rate)
	require.NoError(t, err)
	head, err := p.Header.Encode()
	require.NoError(t, err)
	body := "INBOX"
	m := &models.Message{
		ReceivedAt: time.Now(), SourceID: radio, DestinationID: service, RepeaterID: repeater, Slot: 2, Format: models.MessageFormatTMS,
		ChecksumOK: true, Rate: uint8(rate), Raw: append(head[:], payload...), Text: &body,
	}
	require.NoError(t, database.Create(m).Error)
}

func TestATextGoesInTheFormatOfTheRadiosOwnTextWhereItCameFrom(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	radioSent(t, database, ownRepeater)
	out := &fakeSender{result: data.ResultSent}
	format, err := plugintexts.New(database, out).SendText(context.Background(), service, radio, "1 new message")
	require.NoError(t, err)
	assert.Equal(t, sms.FormatTMS.String(), format)
	got := out.all()
	require.Len(t, got, 1)
	assert.Equal(t, uint(ownRepeater), got[0].repeater)
	assert.True(t, got[0].slot)
	assert.Equal(t, enums.DataTypeRate34, got[0].m.Rate)
	dst, src, ok := got[0].m.Header.Addresses()
	require.True(t, ok)
	assert.Equal(t, []uint32{service, radio}, []uint32{src, dst})
	body, text, err := sms.Decode(nil, &got[0].m.Header, got[0].m.Payload)
	require.NoError(t, err)
	assert.Equal(t, sms.FormatTMS, text.Format)
	assert.Equal(t, "1 new message", string(body))
}

func TestNoTextGoesToARadioWithoutAKnownFormat(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	out := &fakeSender{result: data.ResultSent}
	_, err := plugintexts.New(database, out).SendText(context.Background(), service, radio, "hi")
	require.ErrorIs(t, err, plugins.ErrNoFormat)
	_, err = plugintexts.New(database, out).SendText(context.Background(), service, 1000999, "hi")
	require.ErrorIs(t, err, plugins.ErrNoFormat, "a radio of no user")
	assert.Empty(t, out.all())
}

func TestATextThroughAnUntrustedRepeaterSetsNoFormat(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	radioSent(t, database, otherRepeater)
	out := &fakeSender{result: data.ResultSent}
	_, err := plugintexts.New(database, out).SendText(context.Background(), service, radio, "hi")
	require.ErrorIs(t, err, plugins.ErrNoFormat)
	assert.Empty(t, out.all())
}

func TestTheAnswerOfTheRadioEndsTheSend(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	radioSent(t, database, ownRepeater)
	for result, want := range map[data.Result]error{
		data.ResultRefused:    plugins.ErrRadioRefused,
		data.ResultRetryLimit: plugins.ErrNoAnswer,
		data.ResultTimeLimit:  plugins.ErrNoAnswer,
		data.ResultCancelled:  context.Canceled,
	} {
		_, err := plugintexts.New(database, &fakeSender{result: result}).SendText(context.Background(), service, radio, "hi")
		require.ErrorIs(t, err, want, result)
	}
	format, err := plugintexts.New(database, &fakeSender{result: data.ResultDelivered}).SendText(context.Background(), service, radio, "hi")
	require.NoError(t, err)
	assert.NotEmpty(t, format)
}

func TestATextThatTheFormatCannotCarryIsNotSent(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	radioSentText(t, database, ownRepeater, &sms.Text{Format: sms.FormatDefinedData, DD: enums.DD7Bit, Source: radio, Destination: service, Body: []byte("seven bit")}, enums.DataTypeRate12)
	out := &fakeSender{result: data.ResultSent}
	_, err := plugintexts.New(database, out).SendText(context.Background(), service, radio, "中文")
	require.ErrorIs(t, err, plugins.ErrTextDoesNotFit)
	assert.Empty(t, out.all())
}

// storeText stores text from the radio to the service through repeater on
// timeslot 2 at rate, and returns its id.
func storeText(t *testing.T, database *gorm.DB, repeater uint, text *sms.Text, rate enums.DataType) uint64 {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, rate)
	require.NoError(t, err)
	head, err := p.Header.Encode()
	require.NoError(t, err)
	body := string(text.Body)
	m := &models.Message{
		ReceivedAt: time.Now(), SourceID: uint(text.Source), DestinationID: uint(text.Destination), RepeaterID: repeater, Slot: 2,
		Format: models.MessageFormatTMS, ChecksumOK: true, Rate: uint8(rate), Raw: append(head[:], payload...), Text: &body,
	}
	require.NoError(t, database.Create(m).Error)
	return uint64(m.ID)
}

func TestAReplyGoesInTheExactFormatOfTheTextItAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		text sms.Text
		rate enums.DataType
	}{
		{"TMS", sms.Text{Format: sms.FormatTMS, Confirmed: true, AckRequired: true, Sequence: 3}, enums.DataTypeRate34},
		{"ETSI IP", sms.Text{Format: sms.FormatIP}, enums.DataTypeRate12},
		{"AnyTone", sms.Text{Format: sms.FormatAnyTone, Confirmed: true}, enums.DataTypeRate12},
		{"Defined data", sms.Text{Format: sms.FormatDefinedData, DD: enums.DDISO8859Part1}, enums.DataTypeRate1},
		{"Hytera", sms.Text{Format: sms.FormatHytera, DD: enums.DDBCD}, enums.DataTypeRate12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			database := openDB(t)
			text := tc.text
			text.Source, text.Destination, text.Body = radio, service, []byte("STATUS")
			id := storeText(t, database, ownRepeater, &text, tc.rate)
			radioSentText(t, database, ownRepeater, &sms.Text{Format: sms.FormatTMS, Source: radio, Destination: 4800, Body: []byte("later, in another format")}, enums.DataTypeRate12)
			out := &fakeSender{result: data.ResultSent}
			format, err := plugintexts.New(database, out).ReplyText(context.Background(), service, id, radio, "TS2 linked TG3100")
			require.NoError(t, err)
			assert.Equal(t, tc.text.Format.String(), format)
			got := out.all()
			require.Len(t, got, 1)
			assert.Equal(t, uint(ownRepeater), got[0].repeater)
			assert.True(t, got[0].slot)
			assert.Equal(t, tc.rate, got[0].m.Rate, "the rate of the text")
			var reqHeader sms.Header
			_, err = sms.AppendPacket(nil, &text, &reqHeader)
			require.NoError(t, err)
			assert.Equal(t, reqHeader.Data.Format(), got[0].m.Header.Format(), "the header type of the text")
			dst, src, ok := got[0].m.Header.Addresses()
			require.True(t, ok)
			assert.Equal(t, []uint32{service, radio}, []uint32{src, dst})
			body, reply, err := sms.Decode(nil, &got[0].m.Header, got[0].m.Payload)
			require.NoError(t, err)
			assert.Equal(t, tc.text.Format, reply.Format, "the encoding of the text")
			assert.Equal(t, tc.text.Confirmed, reply.Confirmed)
			assert.Equal(t, tc.text.DD, reply.DD)
			assert.Equal(t, "TS2 linked TG3100", string(body))
		})
	}
}

func TestNoReplyGoesForATextThatTheHubCannotMirror(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	radioSent(t, database, ownRepeater)
	untrusted := storeText(t, database, otherRepeater, &sms.Text{Format: sms.FormatIP, Source: radio, Destination: service, Body: []byte("STATUS")}, enums.DataTypeRate12)
	toOther := storeText(t, database, ownRepeater, &sms.Text{Format: sms.FormatIP, Source: radio, Destination: 4800, Body: []byte("STATUS")}, enums.DataTypeRate12)
	var udt pdu.DataHeader
	require.NoError(t, udt.SetUDT(pdu.UDTHeader{SAP: enums.SAPUnifiedDataTransport, UDTFormat: enums.UDTISO8, LLIDDestination: service, LLIDSource: radio}))
	head, err := udt.Encode()
	require.NoError(t, err)
	body := "STATUS"
	u := &models.Message{ReceivedAt: time.Now(), SourceID: radio, DestinationID: service, RepeaterID: ownRepeater, Slot: 2, Format: models.MessageFormatUDT,
		ChecksumOK: true, Rate: uint8(enums.DataTypeRate12), Raw: append(head[:], []byte("STATUS\x00\x00\x00\x00")...), Text: &body}
	require.NoError(t, database.Create(u).Error)
	out := &fakeSender{result: data.ResultSent}
	texts := plugintexts.New(database, out)
	for name, id := range map[string]uint64{"through an untrusted repeater": untrusted, "to another ID": toOther, "a UDT": uint64(u.ID), "no such text": 999999} {
		_, err := texts.ReplyText(context.Background(), service, id, radio, "TS2")
		require.ErrorIs(t, err, plugins.ErrNoFormat, name)
	}
	_, err = texts.ReplyText(context.Background(), service, untrusted, 1000002, "TS2")
	require.ErrorIs(t, err, plugins.ErrNoFormat, "a text of another radio")
	assert.Empty(t, out.all(), "never a format the radio did not send")
}

func TestATextOfAStreamThatEndedReadsNoFormat(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	radioSent(t, database, ownRepeater)
	out := &fakeSender{result: data.ResultSent}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := plugintexts.New(database, out).SendText(ctx, service, radio, "hi")
	require.ErrorIs(t, err, context.Canceled, "the query of the format ends with the context of the request")
	_, err = plugintexts.New(database, out).ReplyText(ctx, service, 1, radio, "hi")
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, out.all())
}
