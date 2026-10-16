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

package ipsc

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
)

// IPSC packets from a MOTOTRBO repeater: a group voice header and terminator
// for TG 2 from 3120101.
const (
	captureGroupHeader = "800004c313052f9be5000002020000430e0080dd32fe69dc34dd000000000100000a800a00600000000000022f9be50bbebf0011705d"
	captureGroupTerm   = "800004c313052f9be5000002020000430e00805d332c69dc8b1d000000000002000a800a00600000000000022f9be504b1b000107011"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func lc12(t *testing.T, s string) [12]byte {
	t.Helper()
	var lc [12]byte
	copy(lc[:], mustHex(t, s))
	return lc
}

// seal writes the masked RS(12,9) parity of lc[0:9] into lc[9:12].
func seal(t *testing.T, lc *[12]byte, dt enums.DataType) {
	t.Helper()
	if err := pdu.Seal(lc, dt); err != nil {
		t.Fatal(err)
	}
}

func TestFullLCCodewordCaptureVectors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		lc     string
		header string
		term   string
	}{
		{name: "group", lc: "0000000000022f9be5", header: "0bbebf", term: "04b1b0"},
		{name: "private", lc: "0300002f9be52f9f67", header: "783e23", term: "77312c"},
	}
	for _, tc := range tests {
		var cw [12]byte
		copy(cw[:], mustHex(t, tc.lc))
		seal(t, &cw, enums.DataTypeVoiceLCHeader)
		if got := hex.EncodeToString(cw[9:]); got != tc.header {
			t.Errorf("%s: header parity %s, want %s", tc.name, got, tc.header)
		}
		if !pdu.FullLCParityOK(cw, enums.DataTypeVoiceLCHeader) || pdu.FullLCParityOK(cw, enums.DataTypeTerminatorWithLC) {
			t.Errorf("%s: header mask check failed", tc.name)
		}
		seal(t, &cw, enums.DataTypeTerminatorWithLC)
		if got := hex.EncodeToString(cw[9:]); got != tc.term {
			t.Errorf("%s: terminator parity %s, want %s", tc.name, got, tc.term)
		}
	}
}

// TestTranslateToIPSCSynthesizedLCCodeword checks the synthesized Full LC
// against codewords from the RS(12,9) encoder of TS 102 361-1 B.3.6 with
// the Table B.21 masks.
func TestTranslateToIPSCSynthesizedLCCodeword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		group    bool
		src, dst uint
		dt       enums.DataType
		want     string
	}{
		{group: true, src: 0x2F9BE5, dst: 2, dt: enums.DataTypeVoiceLCHeader, want: "0000200000022f9be50f7631"},
		{group: true, src: 0x2F9BE5, dst: 2, dt: enums.DataTypeTerminatorWithLC, want: "0000200000022f9be500793e"},
		{group: false, src: 100, dst: 200, dt: enums.DataTypeVoiceLCHeader, want: "0300200000c8000064fa3725"},
		{group: false, src: 100, dst: 200, dt: enums.DataTypeTerminatorWithLC, want: "0300200000c8000064f5382a"},
	}
	for _, tc := range tests {
		tr := newTestTranslator(t)
		pkt := voiceLCPacket([12]byte{}, tc.dt)
		pkt.Src, pkt.Dst, pkt.GroupCall = tc.src, tc.dst, tc.group
		out := tr.TranslateToIPSC(pkt)
		if len(out) == 0 {
			t.Fatalf("dtype %d: no output", tc.dt)
		}
		if got := hex.EncodeToString(out[0][38:50]); got != tc.want {
			t.Errorf("group=%v dtype %d: LC %s, want %s", tc.group, tc.dt, got, tc.want)
		}
	}
}

func dataBurst(p [12]byte, dt enums.DataType) [33]byte {
	b, err := layer2.BuildDataBurst(p, dt, 0, layer2.SyncSourceBS)
	if err != nil {
		panic(err)
	}
	return b
}

func decodeBPTCPayload(b [33]byte) ([12]byte, bool) {
	p, res := layer2.RawBurst(b).BPTCPayload()
	return p, !res.Uncorrectable
}

// TestTranslateToIPSCDataHeaderVector checks the IPSC payload of an
// unconfirmed data header from 3191868 to group 9 with 2 blocks to follow.
// dmrgo v0.0.1 BPTC(196,96) read rows 1-8 one column late and sent
// 824a00001261787b0401a83b for this burst.
func TestTranslateToIPSCDataHeaderVector(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	var burst [33]byte
	copy(burst[:], mustHex(t, "08dd874c11ff21001564cb21ddadff57d75df5de6a3818ea304a248d80080d809d"))
	pkt := models.Packet{
		Signature:   "DMRD",
		Src:         3191868,
		Dst:         9,
		GroupCall:   true,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(enums.DataTypeDataHeader),
		StreamID:    0x5678,
		DMRData:     burst,
	}
	out := tr.TranslateToIPSC(pkt)
	if len(out) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(out))
	}
	if got := hex.EncodeToString(out[0][38:50]); got != "82a500000930b43c8200d41d" {
		t.Fatalf("header payload %s, want 82a500000930b43c8200d41d", got)
	}
	h := pdu.DataHeaderBytes(out[0][38:50])
	target, source, ok := h.Addresses()
	if !h.CRCOK() || !ok || target != 9 || source != 3191868 {
		t.Fatalf("header CRC ok=%v addresses ok=%v target=%d source=%d", h.CRCOK(), ok, target, source)
	}
	if bf := out[0][46] & 0x7F; bf != 2 {
		t.Fatalf("blocks to follow %d, want 2", bf)
	}

	// The reverse direction must rebuild the same BPTC info bits. The color
	// code differs (7 on air, 0 from DMRHub), so skip the slot type and SYNC.
	back := tr.TranslateToMMDVM(out[0][0], out[0])
	if len(back) != 1 {
		t.Fatalf("expected 1 MMDVM packet, got %d", len(back))
	}
	got := back[0].DMRData
	if !bytes.Equal(got[:12], burst[:12]) || got[12]&0xC0 != burst[12]&0xC0 ||
		got[20]&0x03 != burst[20]&0x03 || !bytes.Equal(got[21:], burst[21:]) {
		t.Fatalf("MMDVM burst %x, want info bits of %x", got, burst)
	}
}

func voiceLCPacket(lc [12]byte, dataType enums.DataType) models.Packet {
	return models.Packet{
		Signature:   "DMRD",
		Src:         0x2F9BE5,
		Dst:         2,
		Repeater:    3001,
		GroupCall:   true,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dataType),
		StreamID:    0x1234,
		DMRData:     dataBurst(lc, dataType),
	}
}

func TestTranslateToIPSCPassesCapturedLCThrough(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	header := mustHex(t, captureGroupHeader)
	term := mustHex(t, captureGroupTerm)

	var hlc, tlc [12]byte
	copy(hlc[:], header[38:50])
	copy(tlc[:], term[38:50])

	headers := tr.TranslateToIPSC(voiceLCPacket(hlc, enums.DataTypeVoiceLCHeader))
	if len(headers) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(headers))
	}
	for _, out := range headers {
		if !bytes.Equal(out[38:50], header[38:50]) {
			t.Fatalf("header LC %x, want %x", out[38:50], header[38:50])
		}
	}
	out := tr.TranslateToIPSC(voiceLCPacket(tlc, enums.DataTypeTerminatorWithLC))
	if len(out) != 1 || !bytes.Equal(out[0][38:50], term[38:50]) {
		t.Fatalf("terminator LC %x, want %x", out[0][38:50], term[38:50])
	}
}

func TestTranslateToIPSCKeepsServiceOptions(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	// Emergency + priority 3 with FID 0x10: a synthesized LC would lose these.
	lc := lc12(t, "001083000002"+"2f9be5"+"000000")
	seal(t, &lc, enums.DataTypeVoiceLCHeader)
	out := tr.TranslateToIPSC(voiceLCPacket(lc, enums.DataTypeVoiceLCHeader))
	if !bytes.Equal(out[0][38:50], lc[:]) {
		t.Fatalf("header LC %x, want %x", out[0][38:50], lc)
	}
}

func TestTranslateToIPSCSynthesizesLC(t *testing.T) {
	t.Parallel()
	good := lc12(t, "0000000000022f9be50bbebf")
	badParity := good
	badParity[11] ^= 0x01
	tests := []struct {
		name string
		lc   [12]byte
		pkt  func(*models.Packet)
	}{
		{name: "zero burst", lc: [12]byte{}},
		{name: "bad parity", lc: badParity},
		// The parrot swaps src and dst without touching the burst.
		{name: "routing changed", lc: good, pkt: func(p *models.Packet) {
			p.Src, p.Dst, p.GroupCall = 2, 0x2F9BE5, false
		}},
	}
	for _, tc := range tests {
		for _, dt := range []enums.DataType{enums.DataTypeVoiceLCHeader, enums.DataTypeTerminatorWithLC} {
			tr := newTestTranslator(t)
			pkt := voiceLCPacket(tc.lc, dt)
			if tc.pkt != nil {
				tc.pkt(&pkt)
			}
			out := tr.TranslateToIPSC(pkt)
			if len(out) == 0 {
				t.Fatalf("%s: no output", tc.name)
			}
			var got [12]byte
			copy(got[:], out[0][38:50])
			if !pdu.FullLCParityOK(got, dt) {
				t.Errorf("%s dtype %d: LC %x has bad parity", tc.name, dt, got)
			}
			if !fullLCMatchesPacket(got, pkt) {
				t.Errorf("%s dtype %d: LC %x does not match packet", tc.name, dt, got)
			}
		}
	}
}

func TestTranslateToMMDVMKeepsCapturedLC(t *testing.T) {
	t.Parallel()
	for _, capture := range []string{captureGroupHeader, captureGroupTerm} {
		tr := newTestTranslator(t)
		data := mustHex(t, capture)
		out := tr.TranslateToMMDVM(data[0], data)
		if len(out) != 1 {
			t.Fatalf("expected 1 packet, got %d", len(out))
		}
		lc, ok := decodeBPTCPayload(out[0].DMRData)
		if !ok || !bytes.Equal(lc[:], data[38:50]) {
			t.Fatalf("burst LC %x ok=%v, want %x", lc, ok, data[38:50])
		}
	}
}

func TestTranslateToMMDVMRecomputesParityAfterFLCOChange(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	data := mustHex(t, captureGroupHeader)
	data[0] = 0x81 // PVT_VOICE carrying a group voice LC
	out := tr.TranslateToMMDVM(data[0], data)
	if len(out) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(out))
	}
	lc, ok := decodeBPTCPayload(out[0].DMRData)
	if !ok {
		t.Fatal("BPTC decode failed")
	}
	if lc[0] != 0x03 {
		t.Fatalf("FLCO 0x%02X, want 0x03", lc[0])
	}
	if !pdu.FullLCParityOK(lc, enums.DataTypeVoiceLCHeader) {
		t.Fatalf("LC %x has bad parity", lc)
	}
}

func TestTranslateToMMDVMShortPacketGetsParity(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	data := makeTestIPSCPacket(0x80, ipscBurstVoiceTerm, true, false)[:40]
	out := tr.TranslateToMMDVM(0x80, data)
	if len(out) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(out))
	}
	lc, ok := decodeBPTCPayload(out[0].DMRData)
	if !ok || !pdu.FullLCParityOK(lc, enums.DataTypeTerminatorWithLC) {
		t.Fatalf("LC %x ok=%v has bad parity", lc, ok)
	}
}

func TestTranslateToIPSCDataCarriesPayload(t *testing.T) {
	t.Parallel()
	payload := lc12(t, "bd00a5a5000064000001c7b1")
	for _, dt := range []enums.DataType{
		enums.DataTypeCSBK, enums.DataTypePIHeader, enums.DataTypeDataHeader,
		enums.DataTypeRate12, enums.DataTypeMBCHeader, enums.DataTypeMBCContinuation,
	} {
		tr := newTestTranslator(t)
		pkt := voiceLCPacket(payload, dt)
		dmrData := pkt.DMRData
		// A voice LC cached earlier in the stream must not leak into data packets.
		tr.TranslateToIPSC(voiceLCPacket(lc12(t, "0000000000022f9be50bbebf"), enums.DataTypeVoiceLCHeader))
		out := tr.TranslateToIPSC(pkt)
		if len(out) != 1 {
			t.Fatalf("dtype %d: expected 1 packet, got %d", dt, len(out))
		}
		if out[0][0] != 0x83 || out[0][30] != byte(dt) {
			t.Fatalf("dtype %d: packet type 0x%02X burst type 0x%02X", dt, out[0][0], out[0][30])
		}
		if !bytes.Equal(out[0][38:50], payload[:]) {
			t.Fatalf("dtype %d: payload %x, want %x", dt, out[0][38:50], payload)
		}
		back := tr.TranslateToMMDVM(out[0][0], out[0])
		if len(back) != 1 || back[0].DMRData != dmrData {
			t.Fatalf("dtype %d: round trip changed the burst", dt)
		}
	}
}

func TestTranslateToIPSCDropsNonBPTCData(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	for _, dt := range []enums.DataType{enums.DataTypeRate34, enums.DataTypeRate1} {
		pkt := makeTestMMDVMPacket(true, false, dmrconst.FrameDataSync, uint(dt))
		if out := tr.TranslateToIPSC(pkt); out != nil {
			t.Fatalf("dtype %d: expected nil, got %d packets", dt, len(out))
		}
	}
}
