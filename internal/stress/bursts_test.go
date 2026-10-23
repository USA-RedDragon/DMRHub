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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
)

// A voice burst carries its identity in the first six octets of its first
// AMBE slot: tagMagic and the index of the burst in its stream. The hub
// does not decode voice for routing, so the tag reaches every target
// unchanged. Other bursts are told apart by their sequence number.
const (
	tagMagic0 = 0xD5
	tagMagic1 = 0x7E
	colorCode = 1
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
	vb, _ := enums.VoiceBurstAt(pos)
	silence := vocoder.AMBESilence()
	raw, err := layer2.BuildVoiceBurst([3]vocoder.AMBEFrame{silence, silence, silence}, vb,
		pdu.EmbeddedSignalling{ColorCode: colorCode}, [4]byte{}, layer2.SyncSourceMS)
	if err != nil {
		return layer2.RawBurst{}, fmt.Errorf("voice burst: %w", err)
	}
	raw[0], raw[1] = tagMagic0, tagMagic1
	binary.BigEndian.PutUint32(raw[2:6], k)
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
	if (p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync) && p.DMRData[0] == tagMagic0 && p.DMRData[1] == tagMagic1 {
		return int(binary.BigEndian.Uint32(p.DMRData[2:6])), true
	}
	return int(p.Seq & 0xFF), false
}
