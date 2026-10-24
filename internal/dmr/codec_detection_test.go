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
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tcTalker      = 1000001
	tcOther       = 1000002
	tcTG          = 91
	tcC2Rpt       = 100101
	tcAMBERpt     = 100102
	tcC2Listen    = 100103
	tcAMBETalkRpt = 100104
	tcIPSC        = 200101
	tcOB          = 3129301
	tcOBCodec2    = 3129302
	tcBursts      = 6 * 3
	tcFrames      = tcBursts * layer2.VoiceSlotsPerBurst
	tcRowWait     = 10 * time.Second
)

// step runs one step of a test whose steps share a network and run in
// order.
func step(t *testing.T, name string, f func(t *testing.T)) {
	t.Helper()
	t.Run(name, f)
}

// voiceCall builds a call with the LC lc and the voice slots, as
// newC2Call does.
func voiceCall(t *testing.T, lc pdu.LCBytes, c call, slots [][9]byte) c2Call {
	t.Helper()
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: layer2.SyncSourceMS})
	require.NoError(t, err)
	out := c2Call{lc: lc, slots: slots, tx: transmission{call: c}}
	add := func(raw layer2.RawBurst, frame dmrconst.FrameType, value uint) {
		out.tx.bursts = append(out.tx.bursts, raw)
		out.tx.frame = append(out.tx.frame, frame)
		out.tx.value = append(out.tx.value, value)
	}
	add(vt.Header(), dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader))
	bursts, err := vt.AddSlots(slots...)
	require.NoError(t, err)
	for i, raw := range bursts {
		frame := dmrconst.FrameVoice
		if i%6 == 0 {
			frame = dmrconst.FrameVoiceSync
		}
		add(raw, frame, uint(i%6))
	}
	add(vt.Terminator(), dmrconst.FrameDataSync, uint(enums.DataTypeTerminatorWithLC))
	return out
}

func tcLC(t *testing.T, c call, fid enums.FID) pdu.LCBytes {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(c.group, c.dst, c.src, c2Options())
	require.NoError(t, err)
	lc.SetFID(fid)
	return lc
}

func tcAMBESlots(seed int) [][9]byte {
	out := make([][9]byte, tcFrames)
	for i := range out {
		var f vocoder.AMBEFrame
		for j := range 6 {
			f[j] = byte((seed+i)*29 + j*83 + 7)
		}
		f[6] = byte((seed+i)<<7) & 0x80
		out[i], _ = vocoder.EncodeAMBE72(f)
	}
	return out
}

type tcRow struct {
	codec                      string
	mode, sentAMBE, sentCodec2 int
	missedMS                   int
}

// requireCodecRow waits for the finished voice row of stream and checks
// its codec and conversion.
func requireCodecRow(t *testing.T, stack *testutils.IntegrationStack, stream uint, want tcRow) {
	t.Helper()
	var got tcRow
	n := 0
	assert.Eventually(t, func() bool {
		var calls []models.Call
		if err := stack.DB.Where("stream_id = ? AND kind = ?", stream, models.CallKindVoice).Find(&calls).Error; err != nil || len(calls) != 1 {
			n = len(calls)
			return false
		}
		n = 1
		c := calls[0]
		got = tcRow{codec: c.Codec, mode: c.Codec2Mode, sentAMBE: c.SentAMBE, sentCodec2: c.SentCodec2, missedMS: c.MissedMS}
		return !c.Active && got == want
	}, tcRowWait, 50*time.Millisecond, "call row of stream %d", stream)
	require.Equal(t, 1, n, "voice rows of stream %d", stream)
	require.Equal(t, want, got, "call row of stream %d", stream)
}

func repeaterOf(t *testing.T, stack *testutils.IntegrationStack, id uint) models.Repeater {
	t.Helper()
	var r models.Repeater
	require.NoError(t, stack.DB.First(&r, id).Error)
	return r
}

// requireHeard waits until timeslot 1 of the repeater id heard codec, in
// mode.
func requireHeard(t *testing.T, stack *testutils.IntegrationStack, id uint, codec string, mode int) models.Repeater {
	t.Helper()
	var r models.Repeater
	assert.Eventually(t, func() bool {
		r = repeaterOf(t, stack, id)
		return r.TS1HeardCodec == codec && r.TS1HeardCodec2Mode == mode
	}, tcRowWait, 20*time.Millisecond)
	require.Equal(t, codec, r.TS1HeardCodec, "codec heard on TS1 of %d", id)
	require.Equal(t, mode, r.TS1HeardCodec2Mode, "Codec2 mode heard on TS1 of %d", id)
	return r
}

// badHeader returns raw, a Voice LC Header, with a bit of its LC flipped
// after the FEC, so that its Reed-Solomon check fails.
func badHeader(raw layer2.RawBurst) layer2.RawBurst {
	p, _ := raw.BPTCPayload()
	p[4] ^= 0x01
	raw.SetBPTCPayload(p)
	return raw
}

// TestCodecDetection sends calls from a hotspot and an OpenBridge peer.
// A call is Codec2 only when it starts with a Voice LC Header with a valid
// LC and the FID 0xC2, and the hotspot timeslot keeps that codec, and its
// Codec2 mode, until an AMBE call. A call without a header takes the codec
// of its timeslot; its voice bits never make it Codec2.
func TestCodecDetection(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, tcTalker, "N0CALL")
		stack.SeedTalkgroup(t, tcTG, "TG")
		stack.SeedMMDVMRepeater(t, tcC2Rpt, tcTalker, testPassword)
		stack.SeedMMDVMRepeater(t, tcAMBERpt, tcTalker, testPassword)
		stack.AssignTS1StaticTG(t, tcAMBERpt, tcTG)
		stack.AssignTS2StaticTG(t, tcAMBERpt, tcTG)
		stack.StartServers(t)
		c2, listen := c2MMDVM(t, stack, tcC2Rpt), c2MMDVM(t, stack, tcAMBERpt)
		ob := c2OBPeer(t, stack, tcOB, false)
		time.Sleep(settleDuration)
		drainQuiet(500*time.Millisecond, c2, listen, ob)

		stream := uint(0xD0000)
		send := func(t *testing.T, from func(models.Packet) error, pkts []models.Packet) {
			t.Helper()
			sendC2(t, from, pkts)
			drainQuiet(callWait(pkts), c2, listen, ob)
		}
		ts1 := call{group: true, dst: tcTG, src: tcTalker}
		var heardAt *time.Time

		step(t, "codec2-header-makes-the-slot-codec2", func(t *testing.T) {
			stream += 16
			cc := newC2Call(t, ts1, false, tcBursts, uint64(stream))
			pkts := cc.tx.packets(stream)
			send(t, c2.SendDMRD, pkts)
			requireCodecRow(t, stack, stream, tcRow{codec: models.CodecCodec2, mode: int(codec2.Mode2400)})
			r := requireHeard(t, stack, tcC2Rpt, models.CodecCodec2, int(codec2.Mode2400))
			require.NotNil(t, r.TS1HeardAt)
			heardAt = r.TS1HeardAt
			assert.Empty(t, r.TS2HeardCodec, "the other slot heard nothing")
		})

		step(t, "a-call-without-header-takes-the-slot-codec", func(t *testing.T) {
			stream += 16
			cc := voiceCall(t, tcLC(t, ts1, enums.FIDStandardized), ts1, tcAMBESlots(1))
			send(t, c2.SendDMRD, cc.tx.packets(stream)[1:])
			requireCodecRow(t, stack, stream, tcRow{codec: models.CodecCodec2})
			r := requireHeard(t, stack, tcC2Rpt, models.CodecCodec2, int(codec2.Mode2400))
			assert.Equal(t, heardAt, r.TS1HeardAt, "a call without a header is not heard as a codec")
		})

		ts2 := ts1
		step(t, "a-header-that-fails-its-check-is-not-codec2", func(t *testing.T) {
			stream += 16
			cc := newC2Call(t, ts2, true, tcBursts, uint64(stream))
			pkts := cc.tx.packets(stream)
			pkts[0].DMRData = badHeader(layer2.RawBurst(pkts[0].DMRData))
			send(t, c2.SendDMRD, pkts)
			requireCodecRow(t, stack, stream, tcRow{codec: models.CodecAMBE})
			assert.Empty(t, repeaterOf(t, stack, tcC2Rpt).TS2HeardCodec)
		})

		step(t, "codec2-voice-bits-without-header-are-not-codec2", func(t *testing.T) {
			stream += 16
			cc := newC2Call(t, ts2, true, tcBursts, uint64(stream))
			pkts := cc.tx.packets(stream)[1:]
			send(t, c2.SendDMRD, pkts)
			requireCodecRow(t, stack, stream, tcRow{codec: models.CodecAMBE})
			assert.Empty(t, repeaterOf(t, stack, tcC2Rpt).TS2HeardCodec)
		})

		step(t, "an-ambe-call-clears-the-slot", func(t *testing.T) {
			stream += 16
			cc := voiceCall(t, tcLC(t, ts1, enums.FIDStandardized), ts1, tcAMBESlots(2))
			send(t, c2.SendDMRD, cc.tx.packets(stream))
			requireCodecRow(t, stack, stream, tcRow{codec: models.CodecAMBE})
			r := requireHeard(t, stack, tcC2Rpt, models.CodecAMBE, int(codec2.Mode2400))
			require.NotNil(t, r.TS1HeardAt)
			assert.True(t, r.TS1HeardAt.After(*heardAt))
			assert.Equal(t, models.CodecAMBE, r.SlotCodec(false))
		})

		step(t, "a-codec2-call-from-a-peer", func(t *testing.T) {
			stream += 16
			cc := newC2Call(t, ts1, false, tcBursts, uint64(stream))
			pkts := cc.tx.packets(stream)
			send(t, func(p models.Packet) error {
				p.Repeater = tcOB
				return ob.SendRawPacket(p)
			}, pkts)
			requireCodecRow(t, stack, stream, tcRow{codec: models.CodecCodec2, mode: int(codec2.Mode2400)})
		})
	})
}
