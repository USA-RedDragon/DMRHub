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

package messages_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	userA     = uint(3191868)
	userB     = uint(3140598)
	repeaterA = uint(319186801)
	tgID      = uint(3100)
)

func memKV(t *testing.T) kv.KV {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.TODO(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = kvStore.Close() })
	return kvStore
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	require.NoError(t, database.Create(&models.User{ID: userA, Callsign: "KI5VMF", Username: "a", Approved: true}).Error)
	require.NoError(t, database.Create(&models.User{ID: userB, Callsign: "KD5SMP", Username: "b", Approved: true}).Error)
	return database
}

type rig struct {
	t     *testing.T
	db    *gorm.DB
	ct    *calltracker.CallTracker
	store *messages.Store
}

func newRig(t *testing.T) *rig {
	t.Helper()
	database := openDB(t)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.TODO(), &cfg)
	require.NoError(t, err)
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	store := messages.NewStore(database, kvStore)
	ct.SetDataObserver(store)
	t.Cleanup(func() {
		ct.Stop()
		store.Stop()
		_ = ps.Close()
		_ = kvStore.Close()
	})
	return &rig{t: t, db: database, ct: ct, store: store}
}

func (r *rig) send(src, dst uint, group bool, stream uint, bursts []layer2.RawBurst) {
	for i, b := range bursts {
		r.ct.Track(context.Background(), models.Packet{
			Signature:   string(dmrconst.CommandDMRD),
			Seq:         uint(i) & 0xff,
			Src:         src,
			Dst:         dst,
			Repeater:    repeaterA,
			Slot:        true,
			GroupCall:   group,
			FrameType:   dmrconst.FrameDataSync,
			DTypeOrVSeq: uint(b.SlotType().DataType),
			StreamID:    stream,
			DMRData:     b,
			BER:         -1,
			RSSI:        -1,
		}, models.RepeaterTypeMMDVM, false)
	}
}

func (r *rig) messages(n int) []models.Message {
	r.t.Helper()
	var out []models.Message
	require.Eventually(r.t, func() bool {
		r.ct.Flush()
		r.store.Flush()
		out = nil
		require.NoError(r.t, r.db.Order("id").Find(&out).Error)
		return len(out) == n
	}, 8*time.Second, 50*time.Millisecond, "want %d messages", n)
	return out
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

func TestStoreDecodesEverySMSFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		format sms.Format
		dd     enums.DDFormat
		want   models.MessageFormat
		body   string
	}{
		{format: sms.FormatTMS, want: models.MessageFormatTMS, body: "tms text"},
		{format: sms.FormatIP, want: models.MessageFormatIP, body: "ip text"},
		{format: sms.FormatAnyTone, want: models.MessageFormatAnyTone, body: "anytone text"},
		{format: sms.FormatDefinedData, dd: enums.DDISO8859Part1, want: models.MessageFormatDefinedData, body: "dd text"},
		{format: sms.FormatHytera, dd: enums.DDBCD, want: models.MessageFormatHytera, body: "hytera"},
	}
	for i, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			r.send(userB, userA, false, uint(7100+i), smsBursts(t, &sms.Text{Format: tc.format, DD: tc.dd, Source: uint32(userB), Destination: uint32(userA), Body: []byte(tc.body)}))
			msgs := r.messages(1)
			m := msgs[0]
			assert.Equal(t, tc.want, m.Format)
			require.NotNil(t, m.Text)
			assert.Equal(t, tc.body, *m.Text)
			assert.Equal(t, userB, m.SourceID)
			assert.Equal(t, userA, m.DestinationID)
			assert.False(t, m.IsGroup)
			assert.Equal(t, repeaterA, m.RepeaterID)
			assert.Equal(t, uint8(2), m.Slot)
			assert.Equal(t, models.RepeaterTypeMMDVM, m.Transport)
			assert.True(t, m.ChecksumOK)
			assert.False(t, m.BadCRC)
			require.Greater(t, len(m.Raw), 12)
			assert.Equal(t, m.Octets, uint(len(m.Raw)-12))
			h := pdu.DecodeDataHeader([12]byte(m.Raw[:12]))
			dst, src, ok := h.Addresses()
			require.True(t, ok)
			assert.Equal(t, uint32(userA), dst)
			assert.Equal(t, uint32(userB), src)
			require.NotNil(t, m.TransmissionID)
			var call models.Call
			require.NoError(t, r.db.First(&call, *m.TransmissionID).Error)
			assert.Equal(t, models.CallKindData, call.Kind)
		})
	}
}

func TestStoreKeepsGroupSMSAndUsersWithStorageOff(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	var u models.User
	require.NoError(t, r.db.First(&u, userA).Error)
	require.False(t, u.SMSStorageEnabled)

	r.send(userA, tgID, true, 7201, smsBursts(t, &sms.Text{Format: sms.FormatIP, Group: true, Source: uint32(userA), Destination: uint32(tgID), Body: []byte("CQ TX")}))
	r.send(userB, userA, false, 7202, smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: uint32(userB), Destination: uint32(userA), Body: []byte("to A")}))
	msgs := r.messages(2)
	assert.True(t, msgs[0].IsGroup)
	assert.Equal(t, tgID, msgs[0].DestinationID)
	assert.False(t, msgs[1].IsGroup)
}

func TestStoreKeepsDataThatIsNotText(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	var h pdu.DataHeader
	require.NoError(t, h.SetDefinedData(pdu.DefinedDataHeader{
		SAP:             enums.SAPShortData,
		DefinedData:     enums.DDBinary,
		LLIDDestination: uint32(userA),
		LLIDSource:      uint32(userB),
		FullMessageFlag: true,
	}))
	p, err := packet.NewPacket(h, []byte{1, 2, 3, 4, 5, 6, 7, 8}, enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	r.send(userB, userA, false, 7301, bursts)
	m := r.messages(1)[0]
	assert.Equal(t, models.MessageFormatData, m.Format)
	assert.Nil(t, m.Text)
	assert.NotEmpty(t, m.Raw)
}

func TestStoreSetsTheACKOfAConfirmedSMS(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	bursts := smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: uint32(userB), Destination: uint32(userA), Confirmed: true, Body: []byte("copy, 5x9")})
	radio, err := data.NewReceiver(&data.ReceiverConfig{Respond: true, Source: layer2.SyncSourceMS})
	require.NoError(t, err)
	var acks []layer2.RawBurst
	var events []data.Event
	for i := range bursts {
		events = radio.Burst(time.Duration(i)*60*time.Millisecond, &bursts[i], layer2.Hint{}, events[:0])
		for _, ev := range events {
			if ev.Kind == data.EventRespond {
				acks = append(acks, ev.Burst)
			}
		}
	}
	require.NotEmpty(t, acks)
	r.send(userB, userA, false, 7401, bursts)
	r.send(userA, userB, false, 7402, acks)
	require.Eventually(t, func() bool {
		m := r.messages(1)[0]
		return m.Response == models.CallResponseACK
	}, 8*time.Second, 50*time.Millisecond)
	m := r.messages(1)[0]
	assert.True(t, m.Confirmed)
	require.NotNil(t, m.SendSequence)
}

func confirmed(t *testing.T, body string, ns uint8) (pdu.DataHeader, []byte) {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Confirmed: true, Body: []byte(body)}, &h)
	require.NoError(t, err)
	c, ok := h.Data.AsConfirmed()
	require.True(t, ok)
	c.SendSequenceNumber = ns
	require.NoError(t, h.Data.SetConfirmed(c))
	return h.Data, payload
}

func delivered(h pdu.DataHeader, payload []byte, bad bool) *data.Event {
	return &data.Event{
		Kind:    data.EventDelivered,
		Call:    data.Call{Source: uint32(userB), Target: uint32(userA)},
		Message: data.Message{Header: h, Payload: payload, BadCRC: bad},
	}
}

func responseEvent(r packet.Response) *data.Event {
	return &data.Event{Kind: data.EventResponse, Call: data.Call{Source: uint32(userB), Target: uint32(userA)}, Response: r}
}

func responseRow(id uint) *models.Call {
	return &models.Call{ID: id, SourceID: userA, DestinationID: userB}
}

func storeOnly(t *testing.T) (*messages.Store, *gorm.DB) {
	t.Helper()
	database := openDB(t)
	store := messages.NewStore(database, memKV(t))
	t.Cleanup(store.Stop)
	return store, database
}

func all(t *testing.T, store *messages.Store, database *gorm.DB) []models.Message {
	t.Helper()
	store.Flush()
	var out []models.Message
	require.NoError(t, database.Order("id").Find(&out).Error)
	return out
}

func TestStoreKeepsOneRowForANACKAndAGoodRetry(t *testing.T) {
	t.Parallel()
	store, database := storeOnly(t)
	now := time.Now()
	h, good := confirmed(t, "retry me", 3)
	bad := append([]byte(nil), good...)
	bad[len(bad)-1] ^= 0xff

	store.DataPacket(&models.Call{ID: 1}, delivered(h, bad, true), now)
	store.DataResponse(responseRow(2), responseEvent(packet.ResponseNACKPacketCRC), now.Add(300*time.Millisecond))
	store.DataPacket(&models.Call{ID: 3}, delivered(h, good, false), now.Add(time.Second))
	store.DataResponse(responseRow(4), responseEvent(packet.ResponseACK), now.Add(1300*time.Millisecond))

	msgs := all(t, store, database)
	require.Len(t, msgs, 1)
	m := msgs[0]
	assert.False(t, m.BadCRC)
	require.NotNil(t, m.Text)
	assert.Equal(t, "retry me", *m.Text)
	assert.Equal(t, models.CallResponseACK, m.Response)
	require.NotNil(t, m.TransmissionID)
	assert.Equal(t, uint(3), *m.TransmissionID)
}

func TestStoreDropsADuplicateRetry(t *testing.T) {
	t.Parallel()
	store, database := storeOnly(t)
	now := time.Now()
	h, payload := confirmed(t, "twice", 5)
	store.DataPacket(&models.Call{ID: 1}, delivered(h, payload, false), now)
	store.DataPacket(&models.Call{ID: 2}, delivered(h, payload, false), now.Add(2*time.Second))
	store.DataPacket(&models.Call{ID: 3}, delivered(h, payload, true), now.Add(3*time.Second))
	assert.Len(t, all(t, store, database), 1)
}

func TestStoreKeepsTwoMessagesWithOtherSequenceNumbers(t *testing.T) {
	t.Parallel()
	store, database := storeOnly(t)
	now := time.Now()
	h1, p1 := confirmed(t, "one", 1)
	h2, p2 := confirmed(t, "two", 2)
	store.DataPacket(&models.Call{ID: 1}, delivered(h1, p1, false), now)
	store.DataPacket(&models.Call{ID: 2}, delivered(h2, p2, false), now.Add(time.Second))
	assert.Len(t, all(t, store, database), 2)
}

func TestStoreAttachesAResponseHeardFirst(t *testing.T) {
	t.Parallel()
	store, database := storeOnly(t)
	now := time.Now()
	h, payload := confirmed(t, "late", 2)
	store.DataResponse(responseRow(9), responseEvent(packet.ResponseACK), now.Add(300*time.Millisecond))
	store.DataPacket(&models.Call{ID: 8}, delivered(h, payload, false), now)
	msgs := all(t, store, database)
	require.Len(t, msgs, 1)
	assert.Equal(t, models.CallResponseACK, msgs[0].Response)
}

// storeReplicas returns two stores that share a database and a KV, as the
// stores of two replicas of the hub do.
func storeReplicas(t *testing.T) (*messages.Store, *messages.Store, *gorm.DB) {
	t.Helper()
	database := openDB(t)
	kvStore := memKV(t)
	one := messages.NewStore(database, kvStore)
	t.Cleanup(one.Stop)
	two := messages.NewStore(database, kvStore)
	t.Cleanup(two.Stop)
	return one, two, database
}

func TestStoreSetsAResponseHeardOnAnotherReplica(t *testing.T) {
	t.Parallel()
	one, two, database := storeReplicas(t)
	now := time.Now()
	h, payload := confirmed(t, "over there", 2)
	one.DataPacket(&models.Call{ID: 1}, delivered(h, payload, false), now)
	one.Flush()
	two.DataResponse(responseRow(2), responseEvent(packet.ResponseACK), now.Add(300*time.Millisecond))
	two.Flush()
	msgs := all(t, one, database)
	require.Len(t, msgs, 1)
	assert.Equal(t, models.CallResponseACK, msgs[0].Response)
}

func TestStoreAttachesAResponseHeardFirstOnAnotherReplica(t *testing.T) {
	t.Parallel()
	one, two, database := storeReplicas(t)
	now := time.Now()
	h, payload := confirmed(t, "late over there", 2)
	two.DataResponse(responseRow(2), responseEvent(packet.ResponseACK), now.Add(300*time.Millisecond))
	two.Flush()
	one.DataPacket(&models.Call{ID: 1}, delivered(h, payload, false), now)
	msgs := all(t, one, database)
	require.Len(t, msgs, 1)
	assert.Equal(t, models.CallResponseACK, msgs[0].Response)
}

func TestStoreReplacesTheFirstFragmentWithTheWholeMessage(t *testing.T) {
	t.Parallel()
	store, database := storeOnly(t)
	now := time.Now()
	var h pdu.DataHeader
	require.NoError(t, h.SetUnconfirmed(pdu.UnconfirmedDataHeader{SAP: enums.SAPShortData, LLIDDestination: uint32(userA), LLIDSource: uint32(userB), FullMessageFlag: true}))
	first := []byte("first part ")
	whole := []byte("first part and the rest")
	store.DataPacket(&models.Call{ID: 1}, &data.Event{Kind: data.EventDelivered, Call: data.Call{Source: uint32(userB), Target: uint32(userA)}, Message: data.Message{Header: h, Payload: first}}, now)
	store.DataPacket(&models.Call{ID: 1}, &data.Event{Kind: data.EventDelivered, Call: data.Call{Source: uint32(userB), Target: uint32(userA)}, Message: data.Message{Header: h, Payload: []byte("and the rest"), Fragmented: true}}, now.Add(time.Second))
	store.DataPacket(&models.Call{ID: 1}, &data.Event{Kind: data.EventReassembled, Call: data.Call{Source: uint32(userB), Target: uint32(userA)}, Message: data.Message{Header: h, Payload: whole, Fragments: 2}}, now.Add(time.Second))
	msgs := all(t, store, database)
	require.Len(t, msgs, 1)
	assert.Equal(t, uint(len(whole)), msgs[0].Octets)
	assert.Equal(t, whole, msgs[0].Raw[12:])
}

func TestStoreNeverBlocksTheTracker(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	release := make(chan struct{})
	require.NoError(t, database.Callback().Create().Before("gorm:create").Register("block", func(*gorm.DB) { <-release }))
	store := messages.NewStore(database, memKV(t))
	t.Cleanup(func() {
		close(release)
		store.Stop()
	})
	h, payload := confirmed(t, "flood", 0)
	start := time.Now()
	for i := range 4096 + 64 {
		store.DataPacket(&models.Call{ID: uint(i + 1)}, delivered(h, payload, false), time.Now())
	}
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.GreaterOrEqual(t, store.Dropped(), uint64(63))
}

func TestStoreKeepsADifferentMessageWithTheSameSequenceNumber(t *testing.T) {
	t.Parallel()
	store, database := storeOnly(t)
	now := time.Now()
	h1, one := confirmed(t, "one", 1)
	h2, two := confirmed(t, "two", 1)
	store.DataPacket(&models.Call{ID: 1}, delivered(h1, one, false), now)
	store.DataPacket(&models.Call{ID: 2}, delivered(h2, two, true), now.Add(time.Second))
	assert.Len(t, all(t, store, database), 2, "a bad copy after a good message is kept")
}

func TestStoreDoesNotReplaceABadMessageWithAnotherLength(t *testing.T) {
	t.Parallel()
	store2, database2 := storeOnly(t)
	now := time.Now()
	h1, one := confirmed(t, "one", 1)
	h3, longer := confirmed(t, "a longer text", 1)
	store2.DataPacket(&models.Call{ID: 1}, delivered(h1, one, true), now)
	store2.DataPacket(&models.Call{ID: 2}, delivered(h3, longer, false), now.Add(time.Second))
	msgs := all(t, store2, database2)
	require.Len(t, msgs, 2, "a good message of another length does not replace a bad one")
	assert.True(t, msgs[0].BadCRC)
	assert.Equal(t, "a longer text", *msgs[1].Text)
}

func TestStoreDoesNotGiveALateResponseToTheNextMessage(t *testing.T) {
	t.Parallel()
	store, database := storeOnly(t)
	now := time.Now()
	h1, one := confirmed(t, "one", 1)
	h2, two := confirmed(t, "two", 2)
	store.DataPacket(&models.Call{ID: 1}, delivered(h1, one, false), now)
	store.DataResponse(responseRow(2), responseEvent(packet.ResponseACK), now.Add(11*time.Second))
	store.DataPacket(&models.Call{ID: 3}, delivered(h2, two, false), now.Add(15*time.Second))
	msgs := all(t, store, database)
	require.Len(t, msgs, 2)
	assert.Equal(t, models.CallResponseNone, msgs[0].Response)
	assert.Equal(t, models.CallResponseNone, msgs[1].Response)
}

func TestStoreGivesNoResponseToAnUnconfirmedMessage(t *testing.T) {
	t.Parallel()
	store, database := storeOnly(t)
	now := time.Now()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("no ack wanted")}, &h)
	require.NoError(t, err)
	store.DataPacket(&models.Call{ID: 1}, delivered(h.Data, payload, false), now)
	store.DataResponse(responseRow(2), responseEvent(packet.ResponseACK), now.Add(time.Second))
	msgs := all(t, store, database)
	require.Len(t, msgs, 1)
	assert.Equal(t, models.CallResponseNone, msgs[0].Response)
}
