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

//go:build stress

package stress_test

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/stretchr/testify/require"
)

// A voice burst carries its identity in the first six octets of its first
// AMBE+2 frame: tagMagic and the index of the burst in its stream. Once
// stampStream has run, its second frame holds streamMagic and the stream.
// The tag
// is in the vocoder bits, under the frame FEC, so it survives IPSC, which
// decodes and encodes the frames again. Other bursts are told apart by
// their sequence number.
const (
	tagMagic0 = 0xD5
	tagMagic1 = 0x7E

	streamMagic0 = 0xA3
	streamMagic1 = 0x5C
	colorCode    = 1
)

// burst is one DMRD packet of a scripted transmission.
type burst struct {
	frame dmrconst.FrameType
	value uint
	raw   layer2.RawBurst
}

func (b burst) voiceFrame() bool {
	return b.frame == dmrconst.FrameVoice || b.frame == dmrconst.FrameVoiceSync
}

func voiceLC(group bool, dst, src uint, dt enums.DataType) (layer2.RawBurst, error) {
	lc := pdu.FullLinkControl{BurstDataType: dt}
	var err error
	if group {
		err = lc.SetGroupVoice(pdu.FLCGroupVoice{GroupAddress: uint32(dst), SourceAddress: uint32(src)}) //nolint:gosec
	} else {
		err = lc.SetUnitToUnit(pdu.FLCUnitToUnit{TargetAddress: uint32(dst), SourceAddress: uint32(src)}) //nolint:gosec
	}
	if err != nil {
		return layer2.RawBurst{}, fmt.Errorf("voice LC: %w", err)
	}
	raw, err := layer2.BuildBurst(&lc, colorCode, layer2.SyncSourceMS)
	if err != nil {
		return layer2.RawBurst{}, fmt.Errorf("voice LC burst: %w", err)
	}
	return raw, nil
}

// voiceFrameBurst is voice burst pos (0 is A) with the tag of index k.
func voiceFrameBurst(pos int, k uint32) (layer2.RawBurst, error) {
	return voiceFrameBurstOf(pos, k, 0)
}

func voiceFrameBurstOf(pos int, k, stream uint32) (layer2.RawBurst, error) {
	vb, _ := enums.VoiceBurstAt(pos)
	silence := vocoder.AMBESilence()
	tag := vocoder.AMBEFrame{tagMagic0, tagMagic1}
	binary.BigEndian.PutUint32(tag[2:6], k)
	second := silence
	if stream != 0 {
		second = vocoder.AMBEFrame{streamMagic0, streamMagic1}
		binary.BigEndian.PutUint32(second[2:6], stream)
	}
	raw, err := layer2.BuildVoiceBurst([3]vocoder.AMBEFrame{tag, second, silence}, vb,
		pdu.EmbeddedSignalling{ColorCode: colorCode}, [4]byte{}, layer2.SyncSourceMS)
	if err != nil {
		return layer2.RawBurst{}, fmt.Errorf("voice burst: %w", err)
	}
	return raw, nil
}

// voiceScript is a voice transmission: a header unless late, superframes
// of n voice bursts from position first, and a terminator unless lost.
type voiceScript struct {
	group      bool
	dst, src   uint
	frames     int
	late       bool
	lateFirst  int
	noTerm     bool
	headerReps int
}

func (v voiceScript) bursts() ([]burst, error) {
	var out []burst
	if !v.late {
		h, err := voiceLC(v.group, v.dst, v.src, enums.DataTypeVoiceLCHeader)
		if err != nil {
			return nil, err
		}
		for range max(v.headerReps, 1) {
			out = append(out, burst{frame: dmrconst.FrameDataSync, value: uint(dmrconst.DTypeVoiceHead), raw: h})
		}
	}
	first := 0
	if v.late {
		first = v.lateFirst
	}
	for i := range v.frames {
		pos := (first + i) % 6
		raw, err := voiceFrameBurst(pos, uint32(len(out))) //nolint:gosec
		if err != nil {
			return nil, err
		}
		f := dmrconst.FrameVoice
		if pos == 0 {
			f = dmrconst.FrameVoiceSync
		}
		out = append(out, burst{frame: f, value: uint(pos), raw: raw})
	}
	if !v.noTerm {
		if len(out)%256 == 0 {
			raw, err := voiceFrameBurst((first+v.frames)%6, uint32(len(out))) //nolint:gosec
			if err != nil {
				return nil, err
			}
			out = append(out, burst{frame: dmrconst.FrameVoice, value: uint((first + v.frames) % 6), raw: raw})
		}
		t, err := voiceLC(v.group, v.dst, v.src, enums.DataTypeTerminatorWithLC)
		if err != nil {
			return nil, err
		}
		out = append(out, burst{frame: dmrconst.FrameDataSync, value: uint(dmrconst.DTypeVoiceTerm), raw: t})
	}
	return out, nil
}

func dataBurst(raw layer2.RawBurst) burst {
	return burst{frame: dmrconst.FrameDataSync, value: uint(raw.SlotType().DataType), raw: raw}
}

func csbkRaw(cs *pdu.CSBK) (layer2.RawBurst, error) {
	cs.LastBlock = true
	p, err := cs.Encode()
	if err != nil {
		return layer2.RawBurst{}, fmt.Errorf("CSBK: %w", err)
	}
	raw, err := layer2.BuildDataBurst(p, enums.DataTypeCSBK, colorCode, layer2.SyncSourceMS)
	if err != nil {
		return layer2.RawBurst{}, fmt.Errorf("CSBK burst: %w", err)
	}
	return raw, nil
}

func preambleBursts(group bool, dst, src uint, n, follow int) ([]burst, error) {
	out := make([]burst, 0, n)
	for i := range n {
		var cs pdu.CSBK
		if err := cs.SetPreamble(pdu.PreamblePDU{
			Data: true, Group: group, CSBKBlocksToFollow: byte(follow + n - 1 - i), //nolint:gosec
			TargetAddress: uint32(dst), SourceAddress: uint32(src), //nolint:gosec
		}); err != nil {
			return nil, fmt.Errorf("preamble: %w", err)
		}
		raw, err := csbkRaw(&cs)
		if err != nil {
			return nil, err
		}
		out = append(out, dataBurst(raw))
	}
	return out, nil
}

// smsScript is an SMS in format f at rate, after five preambles.
func smsBursts(f sms.Format, group bool, dst, src uint, body string, rate enums.DataType, confirmed bool) ([]burst, *packet.Packet, error) {
	text := &sms.Text{Format: f, Source: uint32(src), Destination: uint32(dst), Group: group, Confirmed: confirmed, Body: []byte(body)} //nolint:gosec
	if f == sms.FormatDefinedData {
		text.DD = enums.DDISO8859Part1
	}
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	if err != nil {
		return nil, nil, fmt.Errorf("sms: %w", err)
	}
	p, err := packet.NewPacket(h.Data, payload, rate)
	if err != nil {
		return nil, nil, fmt.Errorf("packet: %w", err)
	}
	raws, err := packet.Bursts(&p, colorCode, layer2.SyncSourceMS)
	if err != nil {
		return nil, nil, fmt.Errorf("bursts: %w", err)
	}
	out, err := preambleBursts(group, dst, src, 5, len(raws))
	if err != nil {
		return nil, nil, err
	}
	for _, r := range raws {
		out = append(out, dataBurst(r))
	}
	return out, &p, nil
}

func responseBursts(p *packet.Packet, r packet.Response) ([]burst, error) {
	h, ok := p.Header.AsConfirmed()
	if !ok {
		return nil, fmt.Errorf("not a confirmed packet")
	}
	resp, err := packet.NewResponse(&h, r, nil)
	if err != nil {
		return nil, fmt.Errorf("response: %w", err)
	}
	raws, err := packet.Bursts(&resp, colorCode, layer2.SyncSourceMS)
	if err != nil {
		return nil, fmt.Errorf("response bursts: %w", err)
	}
	out := make([]burst, 0, len(raws))
	for _, raw := range raws {
		out = append(out, dataBurst(raw))
	}
	return out, nil
}

func singleBPTC(dt enums.DataType, p [12]byte) ([]burst, error) {
	raw, err := layer2.BuildDataBurst(p, dt, colorCode, layer2.SyncSourceMS)
	if err != nil {
		return nil, fmt.Errorf("data burst: %w", err)
	}
	return []burst{dataBurst(raw)}, nil
}

func randomBurst(r *rand.Rand, frame dmrconst.FrameType, value uint) burst {
	var raw layer2.RawBurst
	for i := range raw {
		raw[i] = byte(r.Uint32())
	}
	return burst{frame: frame, value: value, raw: raw}
}

// packetOf is burst k of a stream as a DMRD packet.
func packetOf(b burst, k int, src, dst, stream uint, group, slot2 bool) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         uint(k & 0xFF),
		Src:         src,
		Dst:         dst,
		GroupCall:   group,
		Slot:        slot2,
		FrameType:   b.frame,
		DTypeOrVSeq: b.value,
		StreamID:    stream,
		DMRData:     b.raw,
		BER:         -1,
		RSSI:        -1,
	}
}

// burstIndex is the index in its stream of a received packet: the tag of a
// tagged voice burst, or else -1 and the sequence number.
func burstIndex(p *models.Packet) (int, bool) {
	if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
		if k, ok := voiceTag(layer2.RawBurst(p.DMRData)); ok {
			return k, true
		}
	}
	return int(p.Seq & 0xFF), false
}

// voiceTag is the index that voiceFrameBurst put in raw.
// stampStream returns a copy of bursts whose voice tags name stream, so
// that a burst rebuilt by IPSC still tells its call.
func stampStream(bursts []burst, stream uint) ([]burst, error) {
	out := append([]burst(nil), bursts...)
	for k := range out {
		if !out[k].voiceFrame() {
			continue
		}
		raw, err := voiceFrameBurstOf(int(out[k].value), uint32(k), uint32(stream)) //nolint:gosec
		if err != nil {
			return nil, err
		}
		out[k].raw = raw
	}
	return out, nil
}

func voiceTagStream(raw layer2.RawBurst) (uint, bool) {
	f, _ := raw.AMBEFrames()
	if f[1][0] != streamMagic0 || f[1][1] != streamMagic1 {
		return 0, false
	}
	return uint(binary.BigEndian.Uint32(f[1][2:6])), true
}

func voiceTag(raw layer2.RawBurst) (int, bool) {
	f, _ := raw.AMBEFrames()
	if f[0][0] != tagMagic0 || f[0][1] != tagMagic1 {
		return 0, false
	}
	return int(binary.BigEndian.Uint32(f[0][2:6])), true
}

// TestHarnessVoiceTagSurvivesIPSC checks the harness itself: the tag of a
// voice burst reads back after the burst goes into an IPSC user packet and
// out again, as on the IPSC paths of the hub.
func TestHarnessVoiceTagSurvivesIPSC(t *testing.T) {
	t.Parallel()
	for pos := range 6 {
		raw, err := voiceFrameBurst(pos, uint32(1000+pos)) //nolint:gosec
		require.NoError(t, err)
		vb, _ := enums.VoiceBurstAt(pos)
		var u ipscwire.User
		_, err = u.SetRawBurst(raw, layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: vb})
		require.NoError(t, err)
		back, err := u.RawBurst(0, layer2.SyncSourceBS)
		require.NoError(t, err)
		k, ok := voiceTag(back)
		require.True(t, ok, "burst %d lost its tag", pos)
		require.Equal(t, 1000+pos, k)
	}
}

func TestHarnessCountsLateIPSCBurstsForTheirCall(t *testing.T) {
	t.Parallel()
	l := newLedger()
	script := voiceScript{group: true, dst: 50000, src: 3100015, frames: 12}
	bursts, err := script.bursts()
	require.NoError(t, err)
	calls := []*callRec{
		{stream: 1, kind: "voice", src: script.src, dst: script.dst, group: true},
		{stream: 2, kind: "voice", src: script.src, dst: script.dst, group: true},
	}
	send := func(c *callRec, cc uint32, from, to int) {
		for k := from; k < to; k++ {
			if !bursts[k].voiceFrame() {
				continue
			}
			l.willSend(c, k)
			vb, _ := enums.VoiceBurstAt(int(bursts[k].value))
			u := ipscwire.User{Opcode: ipscwire.OpGroupVoice, CallControl: cc}
			_, err := u.SetRawBurst(bursts[k].raw, layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: vb})
			require.NoError(t, err)
			data, err := u.AppendBinary(nil)
			require.NoError(t, err)
			l.receiveIPSC(ipscRptID, &testutils.IPSCBurst{Src: script.src, Dst: script.dst, GroupCall: true, Data: data})
		}
	}
	require.NoError(t, l.register(calls[0], bursts, []uint{ipscRptID}))
	send(calls[0], 1, 0, len(bursts)/2)
	require.NoError(t, l.register(calls[1], bursts, []uint{ipscRptID}))
	send(calls[0], 1, len(bursts)/2, len(bursts))
	send(calls[1], 2, 0, len(bursts))
	for _, c := range calls {
		for k, b := range bursts {
			if b.voiceFrame() {
				require.Equal(t, uint8(1), c.got[k][0], "stream %d burst %d", c.stream, k)
			}
		}
	}
}

func TestHarnessStreamTagSurvivesIPSC(t *testing.T) {
	t.Parallel()
	bursts, err := voiceScript{group: true, dst: 50000, src: 3100015, frames: 6}.bursts()
	require.NoError(t, err)
	stamped, err := stampStream(bursts, 65600)
	require.NoError(t, err)
	for k, b := range stamped {
		if !b.voiceFrame() {
			continue
		}
		vb, _ := enums.VoiceBurstAt(int(b.value))
		var u ipscwire.User
		_, err = u.SetRawBurst(b.raw, layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: vb})
		require.NoError(t, err)
		back, err := u.RawBurst(0, layer2.SyncSourceBS)
		require.NoError(t, err)
		stream, ok := voiceTagStream(back)
		require.True(t, ok, "burst %d lost its stream", k)
		require.Equal(t, uint(65600), stream)
		idx, ok := voiceTag(back)
		require.True(t, ok)
		require.Equal(t, k, idx)
	}
}

func TestHarnessCountsSlotBusyBurstsAsDropped(t *testing.T) {
	t.Parallel()
	l := newLedger()
	bursts, err := voiceScript{group: true, dst: 50000, src: 3100015, frames: 12}.bursts()
	require.NoError(t, err)
	bursts, err = stampStream(bursts, 7)
	require.NoError(t, err)
	c := &callRec{stream: 7, kind: "voice", src: 3100015, dst: 50000, group: true, loose: map[uint]bool{ipscRptID: true}}
	require.NoError(t, l.register(c, bursts, []uint{ipscRptID}))
	var voice []int
	for k, b := range bursts {
		l.willSend(c, k)
		if b.voiceFrame() {
			voice = append(voice, k)
		}
	}
	c.done.Store(true)
	packet := func(k int) models.Packet {
		return packetOf(bursts[k], k, c.src, c.dst, c.stream, true, false)
	}
	for _, k := range voice[:4] {
		l.countAt(ipscRptID, c, k, l.now(), true)
	}
	for _, k := range voice[3:] {
		p := packet(k)
		p.Repeater = ipscRptID
		l.slotBusy(&p)
	}
	l.finish(0, true)
	tot, _ := l.snapshot()
	require.Equal(t, 0, tot.Missing)
	require.Equal(t, len(voice)-4, tot.SlotBusy)
	require.Equal(t, 1, tot.SlotBusyOK)
	require.Equal(t, int64(0), l.unknown.Load())
}
