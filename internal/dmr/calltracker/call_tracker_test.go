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

package calltracker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	dmrconst "github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	userA      = uint(3191868)
	userB      = uint(3140598)
	strangerID = uint(3999999)
	repeaterA  = uint(100001)
	tgID       = uint(42)
)

func newTracker(t *testing.T) (*calltracker.CallTracker, *gorm.DB, pubsub.PubSub) {
	t.Helper()
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	defConfig.Database.Database = fmt.Sprintf("file:memdb_ct_%p", t)
	defConfig.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	require.NoError(t, err)

	require.NoError(t, database.Create(&models.User{ID: userA, Callsign: "KI5VMF", Username: "a", Approved: true}).Error)
	require.NoError(t, database.Create(&models.User{ID: userB, Callsign: "KD5SMP", Username: "b", Approved: true}).Error)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: repeaterA, Callsign: "RPT1"},
		OwnerID:               userA,
		Type:                  models.RepeaterTypeMMDVM,
	}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: tgID, Name: "TG42"}).Error)

	ct := calltracker.NewCallTracker(database, ps)
	t.Cleanup(func() {
		ct.Stop()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
	})
	return ct, database, ps
}

type sender struct {
	t        *testing.T
	ct       *calltracker.CallTracker
	src, dst uint
	group    bool
	stream   uint
	seq      uint
	server   string
	peer     bool
	repeater uint
}

func newSender(t *testing.T, ct *calltracker.CallTracker, src, dst uint, group bool, stream uint) *sender {
	return &sender{t: t, ct: ct, src: src, dst: dst, group: group, stream: stream, server: models.RepeaterTypeMMDVM, repeater: repeaterA}
}

func (s *sender) packet(frame dmrconst.FrameType, value uint, burst layer2.RawBurst) models.Packet {
	s.seq++
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         s.seq & 0xff,
		Src:         s.src,
		Dst:         s.dst,
		Repeater:    s.repeater,
		Slot:        true,
		GroupCall:   s.group,
		FrameType:   frame,
		DTypeOrVSeq: value,
		StreamID:    s.stream,
		DMRData:     burst,
		BER:         -1,
		RSSI:        -1,
	}
}

func (s *sender) send(frame dmrconst.FrameType, value uint, burst layer2.RawBurst) models.Packet {
	p := s.packet(frame, value, burst)
	s.ct.Track(context.Background(), p, s.server, s.peer)
	return p
}

func (s *sender) data(bursts ...layer2.RawBurst) {
	for _, b := range bursts {
		s.send(dmrconst.FrameDataSync, uint(b.SlotType().DataType), b)
	}
}

func (s *sender) voiceCall(superframes int, term bool) {
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
	for range superframes {
		s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
		for v := uint(1); v <= 5; v++ {
			s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
		}
	}
	if term {
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
	}
}

func burstOf(t *testing.T, d layer2.Data) layer2.RawBurst {
	t.Helper()
	raw, err := layer2.BuildBurst(d, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return raw
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

func preambles(t *testing.T, src, dst uint, n int) []layer2.RawBurst {
	t.Helper()
	out := make([]layer2.RawBurst, 0, n)
	for i := range n {
		var c pdu.CSBK
		c.LastBlock = true
		require.NoError(t, c.SetPreamble(pdu.PreamblePDU{Data: true, CSBKBlocksToFollow: byte(n - 1 - i), TargetAddress: uint32(dst), SourceAddress: uint32(src)}))
		out = append(out, burstOf(t, &c))
	}
	return out
}

func rows(t *testing.T, database *gorm.DB) []models.Call {
	t.Helper()
	var calls []models.Call
	require.NoError(t, database.Order("id").Find(&calls).Error)
	return calls
}

func waitDone(t *testing.T, ct *calltracker.CallTracker, database *gorm.DB, n int) []models.Call {
	t.Helper()
	var calls []models.Call
	require.Eventually(t, func() bool {
		ct.Flush()
		ct.Flush()
		calls = rows(t, database)
		if len(calls) != n {
			return false
		}
		for _, c := range calls {
			if c.Active {
				return false
			}
		}
		return true
	}, 8*time.Second, 50*time.Millisecond, "want %d finished rows", n)
	return calls
}

func TestTrackVoiceCall(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, tgID, true, 7001)
	s.voiceCall(3, false)
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	calls := waitDone(t, ct, database, 1)
	c := calls[0]
	assert.Equal(t, models.CallKindVoice, c.Kind)
	assert.Equal(t, userA, c.SourceID)
	require.NotNil(t, c.UserID)
	assert.Equal(t, userA, *c.UserID)
	require.NotNil(t, c.RepeaterID)
	assert.Equal(t, repeaterA, *c.RepeaterID)
	assert.Equal(t, repeaterA, c.IngressID)
	assert.Equal(t, models.RepeaterTypeMMDVM, c.Transport)
	assert.True(t, c.IsToTalkgroup)
	assert.Zero(t, c.Loss)
	assert.GreaterOrEqual(t, c.Duration, 150*time.Millisecond)
}

func TestTrackVoiceFromUnregisteredSourceOverPeer(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, strangerID, tgID, true, 7002)
	s.server, s.peer, s.repeater = "openbridge", true, 777
	s.voiceCall(2, false)
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	c := waitDone(t, ct, database, 1)[0]
	assert.Nil(t, c.UserID)
	assert.Nil(t, c.RepeaterID)
	assert.Equal(t, strangerID, c.SourceID)
	assert.Equal(t, uint(777), c.IngressID)
	assert.Equal(t, "openbridge", c.Transport)
	assert.True(t, c.IsToTalkgroup)
}

func TestTrackVoiceTimeoutEndsAtTheLastBurst(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, tgID, true, 7003)
	s.voiceCall(1, false)
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})

	c := waitDone(t, ct, database, 1)[0]
	assert.False(t, c.Active)
	assert.Less(t, c.Duration, time.Second, "a timed out call lasts until its last burst")
}

func TestTrackVoiceKeyUpIsDeleted(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, tgID, true, 7004)
	s.voiceCall(0, true)
	ct.Flush()
	time.Sleep(300 * time.Millisecond)
	ct.Flush()
	assert.Empty(t, rows(t, database))
}

func TestTrackEncryptedVoice(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, userB, false, 7005)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
	s.send(dmrconst.FrameDataSync, uint(enums.DataTypePIHeader), layer2.RawBurst{})
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	c := waitDone(t, ct, database, 1)[0]
	assert.Equal(t, models.CallKindVoice, c.Kind)
	assert.Equal(t, "encrypted", c.SubKind)
	assert.True(t, c.IsToUser)
}

func TestTrackRadioCheckAndAnswer(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	var req, ans pdu.CSBK
	req.LastBlock, ans.LastBlock = true, true
	require.NoError(t, req.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))
	require.NoError(t, ans.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{Answer: true, RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))

	s := newSender(t, ct, userA, userB, false, 7006)
	s.data(burstOf(t, &req), burstOf(t, &req))
	ct.Flush()
	back := newSender(t, ct, userB, userA, false, 7007)
	back.data(burstOf(t, &ans))

	c := waitDone(t, ct, database, 1)[0]
	assert.Equal(t, models.CallKindCSBK, c.Kind)
	assert.Equal(t, "radio_check", c.SubKind)
	assert.Equal(t, uint(2), c.Bursts, "a repeated CSBK updates its row")
	require.NotNil(t, c.FID)
	assert.Equal(t, uint8(enums.FIDMotorola), *c.FID)
	assert.Equal(t, models.CallResponseACK, c.Response)
	assert.Len(t, c.CallData, 2*layer2.RawBurstSize, "the row keeps the burst octets")
}

func TestTrackSMSWithPreambles(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userB, userA, false, 7008)
	s.data(preambles(t, userB, userA, 4)...)
	s.data(smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("TEST")})...)

	calls := waitDone(t, ct, database, 2)
	pre, msg := calls[0], calls[1]
	assert.Equal(t, models.CallKindCSBK, pre.Kind)
	assert.Equal(t, "preamble", pre.SubKind)
	assert.Equal(t, uint(4), pre.Bursts)

	assert.Equal(t, models.CallKindData, msg.Kind)
	assert.Equal(t, "sms", msg.SubKind)
	assert.Equal(t, "ip", msg.DataFormat)
	require.NotNil(t, msg.SAP)
	assert.Equal(t, uint8(enums.SAPIPBasedPacketData), *msg.SAP)
	assert.False(t, msg.Confirmed)
	assert.NotZero(t, msg.Octets)
	assert.NotZero(t, msg.Blocks)
	assert.Zero(t, msg.LostBlocks)
	assert.Zero(t, msg.BadCRC)
	assert.True(t, msg.IsToUser)
}

func TestTrackConfirmedSMSWithACK(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

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
	require.NotEmpty(t, acks, "the radio answers the confirmed message")

	s := newSender(t, ct, userB, userA, false, 7009)
	s.data(bursts...)
	back := newSender(t, ct, userA, userB, false, 7010)
	back.data(acks...)

	calls := waitDone(t, ct, database, 2)
	msg, resp := calls[0], calls[1]
	assert.Equal(t, "sms", msg.SubKind)
	assert.Equal(t, "tms", msg.DataFormat)
	assert.True(t, msg.Confirmed)
	assert.Equal(t, models.CallResponseACK, msg.Response)
	assert.Equal(t, models.CallKindData, resp.Kind)
	assert.Equal(t, "response", resp.SubKind)
	assert.Equal(t, models.CallResponseACK, resp.Response)
}

func TestTrackControlKinds(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	var hdr pdu.MBCHeader
	require.NoError(t, hdr.SetBroadcast(pdu.BroadcastPDU{}))
	cont := pdu.MBCContinuation{LastBlock: true}

	s := newSender(t, ct, userA, tgID, true, 7011)
	s.data(burstOf(t, &pdu.PRFill{}))
	s2 := newSender(t, ct, userA, tgID, true, 7012)
	s2.data(burstOf(t, &pdu.UnifiedSingleBlockData{ServiceType: 0}))
	s3 := newSender(t, ct, userA, tgID, true, 7013)
	s3.data(burstOf(t, &hdr), burstOf(t, &cont))

	calls := waitDone(t, ct, database, 3)
	kinds := map[models.CallKind]models.Call{}
	for _, c := range calls {
		kinds[c.Kind] = c
	}
	assert.Contains(t, kinds, models.CallKindIdle)
	assert.Equal(t, "0", kinds[models.CallKindUSBD].SubKind)
	mbc := kinds[models.CallKindMBC]
	assert.Equal(t, uint(2), mbc.Bursts)
	assert.Equal(t, "C_BCAST", mbc.SubKind)
	for _, c := range calls {
		assert.True(t, c.IsToTalkgroup)
		assert.True(t, c.GroupCall)
	}
}

func TestTrackUnknownBursts(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	var junk layer2.RawBurst
	for i := range junk {
		junk[i] = byte(i*37 + 11)
	}
	var vendor pdu.CSBK
	vendor.LastBlock, vendor.Opcode, vendor.FID = true, 0x3A, 0x68

	newSender(t, ct, userA, tgID, true, 7014).send(dmrconst.FrameDataSync, uint(enums.DataTypeCSBK), junk)
	newSender(t, ct, userA, tgID, true, 7015).send(dmrconst.FrameDataSync, 13, junk)
	newSender(t, ct, userA, tgID, true, 7016).send(dmrconst.FrameType(3), 0, junk)
	newSender(t, ct, userA, tgID, true, 7017).data(burstOf(t, &vendor))

	calls := waitDone(t, ct, database, 4)
	subs := map[string]models.Call{}
	for _, c := range calls {
		assert.Equal(t, models.CallKindUnknown, c.Kind)
		subs[c.SubKind] = c
	}
	assert.Contains(t, subs, "bad_crc")
	assert.Contains(t, subs, "reserved")
	assert.Contains(t, subs, "frame_type_3")
	require.Contains(t, subs, "csbk")
	require.NotNil(t, subs["csbk"].Opcode)
	assert.Equal(t, uint8(0x3A), *subs["csbk"].Opcode)
	assert.Equal(t, uint8(0x68), *subs["csbk"].FID)
	assert.Equal(t, junk[:], subs["reserved"].CallData, "an unknown burst keeps its octets")
}

func TestTrackUndeliverable(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, tgID, true, 7018)
	p := s.send(dmrconst.FrameDataSync, uint(enums.DataTypeIdle), burstOf(t, &pdu.PRFill{}))
	ct.Undeliverable(p, "ipsc:311304200")
	ct.Undeliverable(p, "ipsc:311304200")
	ct.Undeliverable(p, "ipsc:311304201")

	c := waitDone(t, ct, database, 1)[0]
	assert.Equal(t, []string{"ipsc:311304200", "ipsc:311304201"}, c.Undeliverable)
}

type observer struct {
	mu       sync.Mutex
	payloads [][]byte
	calls    []uint
}

func (o *observer) DataPacket(call *models.Call, ev *data.Event, _ time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.payloads = append(o.payloads, append([]byte(nil), ev.Message.Payload...))
	o.calls = append(o.calls, call.ID)
}

func (o *observer) DataResponse(*models.Call, *data.Event, time.Time) {}

func TestTrackTellsTheDataObserver(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)
	o := &observer{}
	ct.SetDataObserver(o)

	s := newSender(t, ct, userB, userA, false, 7019)
	s.data(smsBursts(t, &sms.Text{Format: sms.FormatAnyTone, Source: uint32(userB), Destination: uint32(userA), Body: []byte("B5")})...)

	c := waitDone(t, ct, database, 1)[0]
	o.mu.Lock()
	defer o.mu.Unlock()
	require.Len(t, o.payloads, 1)
	assert.NotEmpty(t, o.payloads[0])
	assert.Equal(t, []uint{c.ID}, o.calls)
	assert.Equal(t, "anytone", c.DataFormat)
}

func TestNetCheckInsComeFromVoiceOnly(t *testing.T) {
	t.Parallel()
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	defConfig.Database.Database = fmt.Sprintf("file:memdb_ctnet_%p", t)
	defConfig.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.User{ID: userA, Callsign: "KI5VMF", Username: "a", Approved: true}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: tgID, Name: "TG42"}).Error)
	net := models.Net{TalkgroupID: tgID, StartedByUserID: userA, StartTime: time.Now(), Active: true}
	require.NoError(t, database.Create(&net).Error)
	ct := calltracker.NewCallTracker(database, ps)
	t.Cleanup(func() {
		ct.Stop()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
	})
	sub := ps.Subscribe(fmt.Sprintf("net:checkins:%d", net.ID))
	defer func() { _ = sub.Close() }()

	var c pdu.CSBK
	c.LastBlock = true
	require.NoError(t, c.SetMotorolaCallAlert(pdu.MotorolaCallAlertPDU{}))
	newSender(t, ct, userA, tgID, true, 7020).data(burstOf(t, &c))
	waitDone(t, ct, database, 1)
	select {
	case <-sub.Channel():
		t.Fatal("a CSBK must not check in to a net")
	case <-time.After(200 * time.Millisecond):
	}

	s := newSender(t, ct, userA, tgID, true, 7021)
	s.voiceCall(2, false)
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
	select {
	case <-sub.Channel():
	case <-time.After(5 * time.Second):
		t.Fatal("a voice call checks in to the net")
	}
}

func TestCallUpdatesPublishInOrder(t *testing.T) {
	t.Parallel()
	ct, _, ps := newTracker(t)
	sub := ps.Subscribe("calls")
	defer func() { _ = sub.Close() }()

	s := newSender(t, ct, userA, tgID, true, 7022)
	s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
	time.Sleep(150 * time.Millisecond)
	const count = 40
	for i := range count {
		s.send(dmrconst.FrameVoice, uint(i%6), layer2.RawBurst{})
	}
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	var calls []models.Call
	for len(calls) < count+3 {
		select {
		case msg := <-sub.Channel():
			var c models.Call
			require.NoError(t, json.Unmarshal(msg, &c))
			calls = append(calls, c)
		case <-time.After(3 * time.Second):
			t.Fatalf("got %d of %d call updates", len(calls), count+3)
		}
	}
	for i := 1; i < len(calls); i++ {
		assert.GreaterOrEqual(t, calls[i].Duration, calls[i-1].Duration, "update %d", i)
	}
	for _, c := range calls[:count+2] {
		assert.True(t, c.Active)
	}
	assert.False(t, calls[len(calls)-1].Active, "the last update must end the call")
}

func aliasBlocks(t *testing.T, src uint, alias string) []dmrd.TalkerAlias {
	t.Helper()
	lcs, err := pdu.EncodeTalkerAlias(alias, layer3.TalkerAliasSevenBit)
	require.NoError(t, err)
	out := make([]dmrd.TalkerAlias, 0, len(lcs))
	for i := range lcs {
		out = append(out, dmrd.TalkerAlias{Repeater: uint32(repeaterA), Source: uint32(src), Type: uint8(i), Data: lcs[i].RawPayload, Size: 19})
	}
	return out
}

func TestTrackTalkerAlias(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, tgID, true, 7023)
	s.voiceCall(1, false)
	for _, b := range aliasBlocks(t, userA, "KI5VMF Jacob") {
		ct.TalkerAlias(b, models.RepeaterTypeMMDVM, repeaterA)
	}
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	calls := waitDone(t, ct, database, 2)
	byKind := map[models.CallKind]models.Call{}
	for _, c := range calls {
		byKind[c.Kind] = c
	}
	alias := byKind[models.CallKindAlias]
	assert.Equal(t, "KI5VMF Jacob", alias.TalkerAlias)
	assert.Equal(t, "7-bit", alias.AliasFormat)
	assert.Equal(t, userA, alias.SourceID)
	assert.Equal(t, tgID, alias.DestinationID, "an alias row takes the destination of the call of its source")
	assert.True(t, alias.IsToTalkgroup)
	assert.False(t, alias.Partial)
	assert.Equal(t, "KI5VMF Jacob", byKind[models.CallKindVoice].TalkerAlias, "the alias names the voice call")
}

func TestTrackShortTalkerAliasIsPartial(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	b := aliasBlocks(t, userA, "KI5VMF")[0]
	b.Size = 15
	ct.TalkerAlias(b, models.RepeaterTypeMMDVM, repeaterA)

	c := waitDone(t, ct, database, 1)[0]
	assert.Equal(t, models.CallKindAlias, c.Kind)
	assert.True(t, c.Partial)
	assert.Empty(t, c.TalkerAlias)
}

func TestTrackPosition(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	var lc pdu.FullLinkControl
	require.NoError(t, lc.SetGPSInfo(pdu.FLCGPSInfo{Latitude: 32.75, Longitude: -97.25, PositionError: layer3.PositionLessThan2M}))
	ct.Position(dmrd.Position{Repeater: uint32(repeaterA), Source: uint32(userA), Data: lc.RawPayload, Size: 18}, models.RepeaterTypeMMDVM, repeaterA)
	ct.Position(dmrd.Position{Repeater: uint32(repeaterA), Source: uint32(userB), Size: 14}, models.RepeaterTypeMMDVM, repeaterA)

	calls := waitDone(t, ct, database, 2)
	bySrc := map[uint]models.Call{}
	for _, c := range calls {
		assert.Equal(t, models.CallKindGPS, c.Kind)
		bySrc[c.SourceID] = c
	}
	full := bySrc[userA]
	require.NotNil(t, full.Latitude)
	require.NotNil(t, full.Longitude)
	assert.InDelta(t, 32.75, *full.Latitude, 0.001)
	assert.InDelta(t, -97.25, *full.Longitude, 0.001)
	require.NotNil(t, full.PositionError)
	assert.Equal(t, uint8(layer3.PositionLessThan2M), *full.PositionError)
	short := bySrc[userB]
	assert.True(t, short.Partial)
	assert.Nil(t, short.Latitude, "a short DMRG has no coordinates")
}
