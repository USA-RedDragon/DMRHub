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
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	dmrconst "github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
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
	database, ps, kvStore := trackerDeps(t)
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	t.Cleanup(ct.Stop)
	return ct, database, ps
}

// newReplicas returns two trackers that share the database, pubsub and KV,
// as the trackers of two replicas of the hub do.
func newReplicas(t *testing.T) (*calltracker.CallTracker, *calltracker.CallTracker, *gorm.DB) {
	t.Helper()
	database, ps, kvStore := trackerDeps(t)
	one := calltracker.NewCallTracker(database, ps, kvStore)
	t.Cleanup(one.Stop)
	two := calltracker.NewCallTracker(database, ps, kvStore)
	t.Cleanup(two.Stop)
	return one, two, database
}

func memKV(t *testing.T) kv.KV {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.TODO(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = kvStore.Close() })
	return kvStore
}

func trackerDeps(t *testing.T) (*gorm.DB, pubsub.PubSub, kv.KV) {
	t.Helper()
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	defConfig.Database.Database = fmt.Sprintf("file:memdb_ct_%p", t)
	defConfig.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = kvStore.Close()
	})

	require.NoError(t, database.Create(&models.User{ID: userA, Callsign: "KI5VMF", Username: "a", Approved: true}).Error)
	require.NoError(t, database.Create(&models.User{ID: userB, Callsign: "KD5SMP", Username: "b", Approved: true}).Error)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: repeaterA, Callsign: "RPT1"},
		OwnerID:               userA,
		Type:                  models.RepeaterTypeMMDVM,
	}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: tgID, Name: "TG42"}).Error)
	return database, ps, kvStore
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
	slot     bool
}

func newSender(t *testing.T, ct *calltracker.CallTracker, src, dst uint, group bool, stream uint) *sender {
	return &sender{t: t, ct: ct, src: src, dst: dst, group: group, stream: stream, server: models.RepeaterTypeMMDVM, repeater: repeaterA, slot: true}
}

// newPeerSender returns a sender of an OpenBridge peer, which sends on TS1
// with the peer ID as the repeater.
func newPeerSender(t *testing.T, ct *calltracker.CallTracker, src, dst uint, group bool, stream uint) *sender {
	s := newSender(t, ct, src, dst, group, stream)
	s.server, s.peer, s.repeater, s.slot = "openbridge", true, 777, false
	return s
}

func (s *sender) packet(frame dmrconst.FrameType, value uint, burst layer2.RawBurst) models.Packet {
	s.seq++
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         s.seq & 0xff,
		Src:         s.src,
		Dst:         s.dst,
		Repeater:    s.repeater,
		Slot:        s.slot,
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

func preambles(t *testing.T, n int) []layer2.RawBurst {
	t.Helper()
	out := make([]layer2.RawBurst, 0, n)
	for i := range n {
		var c pdu.CSBK
		c.LastBlock = true
		require.NoError(t, c.SetPreamble(pdu.PreamblePDU{Data: true, CSBKBlocksToFollow: byte(n - 1 - i), TargetAddress: uint32(userA), SourceAddress: uint32(userB)}))
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

// TestTrackVoiceLossOfAMovedCall moves a call from one replica to another
// in the middle of a superframe, as a repeater that moves at a rolling
// restart does. Each replica keeps a row of the part that it heard, and no
// burst was lost, so neither row may count a loss.
func TestTrackVoiceLossOfAMovedCall(t *testing.T) {
	t.Parallel()
	one, two, database := newReplicas(t)

	s := newSender(t, one, userA, tgID, true, 7040)
	s.voiceCall(2, false)
	s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
	s.send(dmrconst.FrameVoice, 1, layer2.RawBurst{})
	s.ct = two
	for v := uint(2); v <= 5; v++ {
		s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
	}
	for range 3 {
		s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
		for v := uint(1); v <= 5; v++ {
			s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
		}
	}
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	for _, c := range waitDone(t, two, database, 2) {
		assert.Zero(t, c.Loss, "the row of the part from %s counts a loss", c.StartTime)
	}
}

// TestTrackVoiceLossIgnoresDuplicates sends the last bursts of a call
// again, as the handover of a move resends what the repeater may not have
// got. The copies are not new bursts, so the call has no loss.
func TestTrackVoiceLossIgnoresDuplicates(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, tgID, true, 7041)
	s.voiceCall(1, false)
	s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
	b := s.send(dmrconst.FrameVoice, 1, layer2.RawBurst{})
	c := s.send(dmrconst.FrameVoice, 2, layer2.RawBurst{})
	for _, p := range []models.Packet{b, c} {
		ct.Track(context.Background(), p, models.RepeaterTypeMMDVM, false)
	}
	for v := uint(3); v <= 5; v++ {
		s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
	}
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	call := waitDone(t, ct, database, 1)[0]
	assert.Zero(t, call.Loss)
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

// TestTrackVoiceFromAPluginLooksUpNoRepeater checks that a call from a
// plugin, which comes from no repeater, does not look up repeater 0, which
// the database logs as a record not found for each call.
func TestTrackVoiceFromAPluginLooksUpNoRepeater(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)
	var mu sync.Mutex
	lookups := 0
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("count_missing_repeaters", func(tx *gorm.DB) {
		if tx.Statement.Table == "repeaters" && errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			mu.Lock()
			defer mu.Unlock()
			lookups++
		}
	}))

	s := newSender(t, ct, 9990, userA, false, 7003)
	s.server, s.repeater = "plugin:1", 0
	s.voiceCall(2, false)
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	c := waitDone(t, ct, database, 1)[0]
	assert.Nil(t, c.RepeaterID)
	assert.Equal(t, "plugin:1", c.Transport)
	assert.True(t, c.IsToUser)
	mu.Lock()
	defer mu.Unlock()
	assert.Zero(t, lookups)
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
	s.data(preambles(t, 4)...)
	s.data(smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("TEST")})...)

	bursts := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("TEST")})
	calls := waitDone(t, ct, database, 1)
	require.Len(t, calls, 1, "the preambles and the data they announce make one row")
	msg := calls[0]
	assert.Equal(t, uint(4+len(bursts)), msg.Bursts)
	assert.Nil(t, msg.Opcode)

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

// TestTrackSMSWithPreamblesOnTheirOwnStreams sends the preambles and the SMS
// that they announce as MMDVMHost does, with a new stream ID for each CSBK
// and data header. They must make one row, as on one stream.
func TestTrackSMSWithPreamblesOnTheirOwnStreams(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	for i, b := range preambles(t, 4) {
		newSender(t, ct, userB, userA, false, uint(7101+i)).data(b)
	}
	bursts := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("TEST")})
	newSender(t, ct, userB, userA, false, 7105).data(bursts...)

	calls := waitDone(t, ct, database, 1)
	msg := calls[0]
	assert.Equal(t, models.CallKindData, msg.Kind)
	assert.Equal(t, "sms", msg.SubKind)
	assert.Equal(t, uint(4+len(bursts)), msg.Bursts)
	assert.Equal(t, uint(7105), msg.StreamID)
	assert.Nil(t, msg.Opcode)
}

// TestTrackPreamblesOfOtherStreamsStayApart checks that a preamble or data
// header on another stream does not join a preamble row when it is late or
// names another call.
func TestTrackPreamblesOfOtherStreamsStayApart(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	pre := preambles(t, 1)[0]
	newSender(t, ct, userB, userA, false, 7111).data(pre)
	newSender(t, ct, userA, userB, false, 7112).data(pre)
	ct.Flush()
	time.Sleep(450 * time.Millisecond)
	newSender(t, ct, userB, userA, false, 7113).data(smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("LATE")})...)

	calls := waitDone(t, ct, database, 3)
	assert.Equal(t, "preamble", calls[0].SubKind)
	assert.Equal(t, "preamble", calls[1].SubKind)
	assert.Equal(t, "sms", calls[2].SubKind)
}

// TestTrackSMSWithPreamblesOnTheirOwnStreamsOverOpenBridge sends the
// preambles and the SMS that they announce as an OpenBridge peer forwards
// them from MMDVMHost, with a new stream ID for each CSBK and data header.
// They must make one row, as from a repeater.
func TestTrackSMSWithPreamblesOnTheirOwnStreamsOverOpenBridge(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	for i, b := range preambles(t, 4) {
		newPeerSender(t, ct, userB, userA, false, uint(7121+i)).data(b)
	}
	bursts := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("TEST")})
	newPeerSender(t, ct, userB, userA, false, 7125).data(bursts...)

	calls := waitDone(t, ct, database, 1)
	msg := calls[0]
	assert.Equal(t, models.CallKindData, msg.Kind)
	assert.Equal(t, "sms", msg.SubKind)
	assert.Equal(t, uint(4+len(bursts)), msg.Bursts)
	assert.Equal(t, uint(7125), msg.StreamID)
	assert.Equal(t, "openbridge", msg.Transport)
	assert.Equal(t, uint(777), msg.IngressID)
	assert.Nil(t, msg.Opcode)
}

// TestTrackInterleavedPreamblesOverOpenBridgeStayApart sends the preambles
// of two calls on their own streams in turn, over OpenBridge. Each call
// must keep its own row, and a late SMS must not join a preamble row.
func TestTrackInterleavedPreamblesOverOpenBridgeStayApart(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	pre := preambles(t, 2)
	stream := uint(7131)
	next := func() uint { stream++; return stream }
	for _, b := range pre {
		newPeerSender(t, ct, userB, userA, false, next()).data(b)
		newPeerSender(t, ct, userA, userB, false, next()).data(b)
		newPeerSender(t, ct, userB, tgID, true, next()).data(b)
	}
	toA := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("TO A")})
	toB := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userA), Destination: uint32(userB), Body: []byte("TO B")})
	newPeerSender(t, ct, userB, userA, false, next()).data(toA...)
	newPeerSender(t, ct, userA, userB, false, next()).data(toB...)
	ct.Flush()
	time.Sleep(450 * time.Millisecond)
	newPeerSender(t, ct, userB, tgID, true, next()).data(smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(tgID), Body: []byte("LATE")})...)

	calls := waitDone(t, ct, database, 4)
	byKey := map[string]models.Call{}
	for _, c := range calls {
		byKey[fmt.Sprintf("%d>%d:%s", c.SourceID, c.DestinationID, c.SubKind)] = c
	}
	require.Len(t, byKey, 4, "rows: %v", byKey)
	assert.Equal(t, uint(len(pre)+len(toA)), byKey[fmt.Sprintf("%d>%d:sms", userB, userA)].Bursts)
	assert.Equal(t, uint(len(pre)+len(toB)), byKey[fmt.Sprintf("%d>%d:sms", userA, userB)].Bursts)
	assert.Equal(t, uint(len(pre)), byKey[fmt.Sprintf("%d>%d:preamble", userB, tgID)].Bursts)
	assert.Contains(t, byKey, fmt.Sprintf("%d>%d:sms", userB, tgID))
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
	assert.Equal(t, uint(len(bursts)-1), msg.Blocks, "the message row counts its own blocks")
	assert.Zero(t, resp.Blocks, "an ACK has no blocks")
}

func confirmedACK(t *testing.T, bursts []layer2.RawBurst) []layer2.RawBurst {
	t.Helper()
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
	return acks
}

// TestTrackAnswersARowOfAnotherReplica sends a confirmed SMS and a radio
// check to one tracker and their answers to another tracker that shares its
// database, pubsub and KV, as when the answering radio is on a repeater of
// another replica. The answers must be set on the rows of the first one.
func TestTrackAnswersARowOfAnotherReplica(t *testing.T) {
	t.Parallel()
	one, two, database := newReplicas(t)

	bursts := smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: uint32(userB), Destination: uint32(userA), Confirmed: true, Body: []byte("copy, 5x9")})
	newSender(t, one, userB, userA, false, 7060).data(bursts...)
	one.Flush()
	newSender(t, two, userA, userB, false, 7061).data(confirmedACK(t, bursts)...)

	var req, ans pdu.CSBK
	req.LastBlock, ans.LastBlock = true, true
	require.NoError(t, req.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))
	require.NoError(t, ans.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{Answer: true, RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))
	newSender(t, one, userA, userB, false, 7062).data(burstOf(t, &req))
	one.Flush()
	newSender(t, two, userB, userA, false, 7063).data(burstOf(t, &ans))

	byStream := map[uint]models.Call{}
	require.Eventually(t, func() bool {
		two.Flush()
		one.Flush()
		clear(byStream)
		for _, c := range rows(t, database) {
			if c.Active {
				return false
			}
			byStream[c.StreamID] = c
		}
		return len(byStream) == 3 && byStream[7060].Response != models.CallResponseNone && byStream[7062].Response != models.CallResponseNone
	}, 8*time.Second, 50*time.Millisecond, "the answers are set on the rows")
	assert.Equal(t, "sms", byStream[7060].SubKind)
	assert.Equal(t, models.CallResponseACK, byStream[7060].Response)
	assert.Equal(t, "response", byStream[7061].SubKind)
	assert.Equal(t, "radio_check", byStream[7062].SubKind)
	assert.Equal(t, models.CallResponseACK, byStream[7062].Response)
	assert.NotContains(t, byStream, uint(7063), "the answer makes no row of its own")
}

func TestTrackResponseAnswersOnlyItsConfirmedMessage(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	newSender(t, ct, userA, userB, false, 7030).data(smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userA), Destination: uint32(userB), Body: []byte("one")})...)
	waitDone(t, ct, database, 1)
	bursts := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Confirmed: true, Body: []byte("two")})
	newSender(t, ct, userB, userA, false, 7031).data(bursts...)
	newSender(t, ct, userA, userB, false, 7032).data(confirmedACK(t, bursts)...)

	calls := waitDone(t, ct, database, 3)
	assert.Equal(t, models.CallResponseNone, calls[0].Response, "an unconfirmed message gets no response")
	assert.Equal(t, models.CallResponseACK, calls[1].Response)
	assert.Equal(t, "response", calls[2].SubKind)
}

func TestTrackTwoMessagesMakeTwoRows(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userB, userA, false, 7033)
	s.data(smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("first")})...)
	s.data(smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: uint32(userB), Destination: uint32(userA), Body: []byte("second")})...)

	calls := waitDone(t, ct, database, 2)
	assert.Equal(t, "ip", calls[0].DataFormat)
	assert.Equal(t, "tms", calls[1].DataFormat)
}

func TestUndeliverableDoesNotBringBackAKeyUp(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, tgID, true, 7034)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
	term := s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
	ct.Undeliverable(term, "ipsc:1")
	ct.Flush()
	ct.Flush()
	time.Sleep(300 * time.Millisecond)
	ct.Flush()
	assert.Empty(t, rows(t, database))
}

func TestDuplicateVoicePacketsAreNotStored(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userA, tgID, true, 7035)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
	dup := s.packet(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
	for range 3 {
		ct.Track(context.Background(), dup, models.RepeaterTypeMMDVM, false)
	}
	time.Sleep(150 * time.Millisecond)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	c := waitDone(t, ct, database, 1)[0]
	assert.Len(t, c.CallData, 3*layer2.RawBurstSize)
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
	assert.Equal(t, "location_information_protocol", kinds[models.CallKindUSBD].SubKind)
	mbc := kinds[models.CallKindMBC]
	assert.Equal(t, uint(2), mbc.Bursts)
	assert.Equal(t, "c_bcast", mbc.SubKind)
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
	assert.Equal(t, junk[:], []byte(subs["reserved"].CallData), "an unknown burst keeps its octets")
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

func TestUndeliverableGoesToTheRowOfItsBurst(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	s := newSender(t, ct, userB, userA, false, 7036)
	bursts := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("first")})
	ids := make([]uint64, 0, len(bursts))
	var last models.Packet
	for _, b := range bursts {
		last = s.packet(dmrconst.FrameDataSync, uint(b.SlotType().DataType), b)
		ids = append(ids, ct.Track(context.Background(), last, models.RepeaterTypeMMDVM, false))
	}
	s.data(smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: uint32(userB), Destination: uint32(userA), Body: []byte("second")})...)
	first := last
	first.TrackID = ids[0]
	ct.Undeliverable(first, "ipsc:1")
	foreign := last
	foreign.TrackID = ids[0] - 1<<20
	ct.Undeliverable(foreign, "ipsc:2")

	calls := waitDone(t, ct, database, 2)
	assert.Equal(t, []string{"ipsc:1"}, calls[0].Undeliverable)
	assert.Empty(t, calls[1].Undeliverable)
}

func TestUndeliverableOnAnotherReplicaGoesToTheRowOfItsBurst(t *testing.T) {
	t.Parallel()
	one, two, database := newReplicas(t)

	s := newSender(t, one, userB, userA, false, 7065)
	bursts := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("first")})
	var first models.Packet
	for i, b := range bursts {
		p := s.packet(dmrconst.FrameDataSync, uint(b.SlotType().DataType), b)
		p.TrackID = one.Track(context.Background(), p, models.RepeaterTypeMMDVM, false)
		if i == 0 {
			first = p
		}
	}
	s.data(smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: uint32(userB), Destination: uint32(userA), Body: []byte("second")})...)
	two.Undeliverable(first, "ipsc:1")

	var calls []models.Call
	require.Eventually(t, func() bool {
		calls = waitDone(t, one, database, 2)
		return len(calls[0].Undeliverable) > 0
	}, 3*time.Second, 50*time.Millisecond, "the mark that replica two made never reached the row on replica one")
	assert.Equal(t, []string{"ipsc:1"}, calls[0].Undeliverable)
	assert.Empty(t, calls[1].Undeliverable)
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
	net := models.Net{TalkgroupID: tgID, StartedByUserID: new(userA), StartTime: time.Now(), Active: true}
	require.NoError(t, database.Create(&net).Error)
	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	require.NoError(t, err)
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	t.Cleanup(func() {
		ct.Stop()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = kvStore.Close()
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

func TestPublicCallsMaskSMS(t *testing.T) {
	t.Parallel()
	ct, database, ps := newTracker(t)
	sub := ps.Subscribe("calls:public")
	defer func() { _ = sub.Close() }()

	s := newSender(t, ct, userB, tgID, true, 7036)
	s.data(smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(tgID), Group: true, Body: []byte("net at 8")})...)
	c := waitDone(t, ct, database, 1)[0]
	require.Equal(t, "sms", c.SubKind)

	select {
	case msg := <-sub.Channel():
		var row map[string]any
		require.NoError(t, json.Unmarshal(msg, &row))
		assert.Equal(t, "data", row["kind"])
		assert.Empty(t, row["sub_kind"])
		assert.InDelta(t, 0, row["octets"], 0)
	case <-time.After(5 * time.Second):
		t.Fatal("no public row")
	}
}

func TestSubKindNamesComeFromDMRGo(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		pdu.CSBKBSOutboundActivation.String():   "bs_outbound_activation_or_td_grant_mi",
		pdu.CSBKPreamble.String():               "preamble",
		pdu.CSBKCapacityPlusSiteStatus.String(): "capacity_plus_site_status",
		enums.SAPShortData.String():             "short_data",
		pdu.ServiceTypeLIP.String():             "location_information_protocol",
		sms.FormatDefinedData.String():          "defined_data",
	} {
		assert.Equal(t, want, calltracker.SubKindName(name), name)
	}
}

func TestBackToBackTransmissionsOnOneSlotEachGetTheirOwnRow(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	var check pdu.CSBK
	check.LastBlock = true
	require.NoError(t, check.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: uint32(userB), CheckedAddress: uint32(userA)}))
	text := func(body string) []layer2.RawBurst {
		return smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte(body)})
	}
	sends := []struct {
		stream uint
		bursts []layer2.RawBurst
		kind   models.CallKind
	}{
		{65600, burstsOf(burstOf(t, &check)), models.CallKindCSBK},
		{65601, text("ONE"), models.CallKindData},
		{65602, append(preambles(t, 2), text("TWO")...), models.CallKindData},
		{65603, burstsOf(burstOf(t, &check)), models.CallKindCSBK},
		{65604, text("THREE"), models.CallKindData},
	}
	for _, s := range sends {
		newSender(t, ct, userB, userA, false, s.stream).data(s.bursts...)
	}

	calls := waitDone(t, ct, database, len(sends))
	for i, s := range sends {
		assert.Equal(t, s.stream, calls[i].StreamID, "row %d", i)
		assert.Equal(t, s.kind, calls[i].Kind, "stream %d", s.stream)
		assert.Equal(t, uint(len(s.bursts)), calls[i].Bursts, "stream %d", s.stream)
	}
}

func TestTheEndOfAMessageDoesNotSplitTheNextOnTheSlot(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	text := func(body string) []layer2.RawBurst {
		return smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: uint32(userB), Destination: uint32(userA), Body: []byte(body)})
	}
	newSender(t, ct, userB, userA, false, 65558).data(append(preambles(t, 5), text("STRESS 9")...)...)
	next := newSender(t, ct, userB, userA, false, 65559)
	bursts := append(preambles(t, 5), text(strings.Repeat("STRESS 10 ", 12))...)
	require.Greater(t, len(bursts), 25)
	for _, b := range bursts {
		next.data(b)
		time.Sleep(60 * time.Millisecond)
	}

	calls := waitDone(t, ct, database, 2)
	byStream := map[uint]int{}
	for _, c := range calls {
		byStream[c.StreamID]++
	}
	assert.Equal(t, map[uint]int{65558: 1, 65559: 1}, byStream, "one row for each stream")
}

func TestASlowStoreDoesNotSplitTransmissions(t *testing.T) {
	t.Parallel()
	database, ps, kvStore := trackerDeps(t)
	require.NoError(t, database.Callback().Create().Before("gorm:create").Register("slow_store", func(tx *gorm.DB) {
		if c, ok := tx.Statement.Dest.(*models.Call); ok && c.Kind == models.CallKindCSBK {
			time.Sleep(1300 * time.Millisecond)
		}
	}))
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	t.Cleanup(ct.Stop)

	const n = 6
	for i := range uint(n) {
		s := newSender(t, ct, userB, userA, false, 65900+i)
		for _, b := range append(preambles(t, 5), smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("slow")})...) {
			s.data(b)
			time.Sleep(60 * time.Millisecond)
		}
	}

	var calls []models.Call
	require.Eventually(t, func() bool {
		ct.Flush()
		ct.Flush()
		calls = rows(t, database)
		for _, c := range calls {
			if c.Active {
				return false
			}
		}
		return len(calls) >= n
	}, 30*time.Second, 50*time.Millisecond)
	byStream := map[uint][]models.CallKind{}
	for _, c := range calls {
		byStream[c.StreamID] = append(byStream[c.StreamID], c.Kind)
	}
	for i := range uint(n) {
		assert.Equal(t, []models.CallKind{models.CallKindData}, byStream[65900+i], "stream %d", 65900+i)
	}
}

func burstsOf(b ...layer2.RawBurst) []layer2.RawBurst {
	return b
}

func TestAnotherStreamOnTheSlotDoesNotSplitAMessage(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	bursts := smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Body: []byte("split me not")})
	var check pdu.CSBK
	check.LastBlock = true
	require.NoError(t, check.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))

	msg := newSender(t, ct, userB, userA, false, 65700)
	other := newSender(t, ct, userA, userB, false, 65701)
	half := len(bursts) / 2
	msg.data(bursts[:half]...)
	other.data(burstOf(t, &check))
	msg.data(bursts[half:]...)

	calls := waitDone(t, ct, database, 2)
	byStream := map[uint]models.Call{}
	for _, c := range calls {
		byStream[c.StreamID] = c
	}
	require.Len(t, byStream, 2, "one row for each stream")
	assert.Equal(t, models.CallKindData, byStream[65700].Kind)
	assert.Equal(t, "sms", byStream[65700].SubKind)
	assert.Equal(t, uint(len(bursts)), byStream[65700].Bursts, "the message keeps all its bursts")
	assert.Equal(t, models.CallKindCSBK, byStream[65701].Kind)
}

func TestVoiceRowRecordsTheFIDOfItsLC(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)

	header := burstOf(t, &pdu.FullLinkControl{
		BurstDataType: enums.DataTypeVoiceLCHeader,
		FLCO:          enums.FLCOGroupVoiceChannelUser,
		FID:           enums.FIDMotorola,
	})
	s := newSender(t, ct, userA, 9, true, 65800)
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), header)
	time.Sleep(150 * time.Millisecond)
	for range 3 {
		s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
		for v := uint(1); v <= 5; v++ {
			s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
		}
	}
	s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

	c := waitDone(t, ct, database, 1)[0]
	assert.Equal(t, models.CallKindVoice, c.Kind)
	require.NotNil(t, c.FID, "the voice row records the FID of its LC")
	assert.Equal(t, uint8(enums.FIDMotorola), *c.FID)
}

func TestCallUpdatesLeaveOutRepeatersOfUsers(t *testing.T) {
	t.Parallel()
	ct, _, ps := newTracker(t)
	sub := ps.Subscribe("calls")
	defer func() { _ = sub.Close() }()

	newSender(t, ct, userA, tgID, true, 7030).voiceCall(1, true)
	newSender(t, ct, userB, userA, false, 7031).voiceCall(1, true)

	seen := map[uint]bool{}
	for len(seen) < 2 {
		select {
		case msg := <-sub.Channel():
			var c struct {
				SourceID uint `json:"source_id"`
				User     struct {
					ID        uint              `json:"id"`
					Repeaters []json.RawMessage `json:"repeaters"`
				} `json:"user"`
				ToUser struct {
					Repeaters []json.RawMessage `json:"repeaters"`
				} `json:"to_user"`
			}
			require.NoError(t, json.Unmarshal(msg, &c))
			require.NotZero(t, c.User.ID, "an update must still name the user")
			assert.Empty(t, c.User.Repeaters, "an update of a call from %d carries the repeaters of its user", c.SourceID)
			assert.Empty(t, c.ToUser.Repeaters, "an update of a call from %d carries the repeaters of the user it calls", c.SourceID)
			seen[c.SourceID] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("got updates of %d of 2 calls", len(seen))
		}
	}
}

type pluginIDs []models.PluginRange

func (p pluginIDs) Owner(id uint) (uint, bool) {
	for i, r := range p {
		if r.Contains(id) {
			return uint(i + 1), true
		}
	}
	return 0, false
}

// TestTrackGroupCallToAPluginIsOnLastHeard sends group calls to the parrot
// and the location server, which have no talkgroup. They go on the public
// last heard as calls to a plugin, with no lookup of a repeater.
func TestTrackGroupCallToAPluginIsOnLastHeard(t *testing.T) {
	t.Parallel()
	ct, database, _ := newTracker(t)
	ct.SetPlugins(pluginIDs{{First: 9990, Last: 9990}, {First: 5050, Last: 5059}})
	var mu sync.Mutex
	lookups := 0
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("count_missing_repeaters", func(tx *gorm.DB) {
		if tx.Statement.Table == "repeaters" && errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			mu.Lock()
			defer mu.Unlock()
			lookups++
		}
	}))

	for i, dst := range []uint{9990, 5055} {
		s := newSender(t, ct, userA, dst, true, uint(7101+i))
		s.slot = i == 0
		s.voiceCall(3, false)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
	}

	calls := waitDone(t, ct, database, 2)
	for _, c := range calls {
		assert.True(t, c.IsToPlugin, c.DestinationID)
		assert.False(t, c.IsToTalkgroup, c.DestinationID)
	}
	heard, err := models.FindCalls(database)
	require.NoError(t, err)
	assert.Len(t, heard, 2)
	n, err := models.CountCalls(database)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	mu.Lock()
	defer mu.Unlock()
	assert.Zero(t, lookups)
}
