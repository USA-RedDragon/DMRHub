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
	"math/rand/v2"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/dmr/layer2"
	"github.com/USA-RedDragon/dmrgo/dmr/layer2/elements"
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

func TestRS129ParityTableB18(t *testing.T) {
	t.Parallel()
	// TS 102 361-1 Table B.18: parity columns of the generator matrix rows.
	rows := []string{"1cbcfd", "893108", "ad4136", "7d7116", "f3a63a", "08837b", "3f6f02", "6c0da7", "0e3840"}
	for i, want := range rows {
		var m [9]byte
		m[i] = 0x01
		p := rs129Parity(m[:])
		if got := hex.EncodeToString(p[:]); got != want {
			t.Errorf("row %d: parity %s, want %s", i, got, want)
		}
	}
}

func TestRS129ParityLinear(t *testing.T) {
	t.Parallel()
	// c = m x G, so the parity of any message is the XOR of scaled rows.
	rng := rand.New(rand.NewPCG(1, 2))
	for n := 0; n < 200; n++ {
		var m [9]byte
		for i := range m {
			m[i] = byte(rng.UintN(256))
		}
		var want [3]byte
		for i := range m {
			var unit [9]byte
			unit[i] = 1
			row := rs129Parity(unit[:])
			for j := range want {
				want[j] ^= gfMul(m[i], row[j])
			}
		}
		if got := rs129Parity(m[:]); got != want {
			t.Fatalf("message %x: parity %x, want %x", m, got, want)
		}
	}
}

func TestRS129ParityCaptureVectors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		lc     string
		parity string
		header string
		term   string
	}{
		{name: "group", lc: "0000000000022f9be5", parity: "9d2829", header: "0bbebf", term: "04b1b0"},
		{name: "private", lc: "0300002f9be52f9f67", parity: "eea8b5", header: "783e23", term: "77312c"},
	}
	for _, tc := range tests {
		lc := mustHex(t, tc.lc)
		p := rs129Parity(lc)
		if got := hex.EncodeToString(p[:]); got != tc.parity {
			t.Errorf("%s: parity %s, want %s", tc.name, got, tc.parity)
		}
		var cw [12]byte
		copy(cw[:], lc)
		setFullLCParity(&cw, elements.DataTypeVoiceLCHeader)
		if got := hex.EncodeToString(cw[9:]); got != tc.header {
			t.Errorf("%s: header parity %s, want %s", tc.name, got, tc.header)
		}
		if !fullLCParityOK(cw, elements.DataTypeVoiceLCHeader) || fullLCParityOK(cw, elements.DataTypeTerminatorWithLC) {
			t.Errorf("%s: header mask check failed", tc.name)
		}
		setFullLCParity(&cw, elements.DataTypeTerminatorWithLC)
		if got := hex.EncodeToString(cw[9:]); got != tc.term {
			t.Errorf("%s: terminator parity %s, want %s", tc.name, got, tc.term)
		}
	}
}

func TestDecodeBPTCPayloadRoundTrip(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(3, 4))
	for n := 0; n < 100; n++ {
		var payload [12]byte
		for i := range payload {
			payload[i] = byte(rng.UintN(256))
		}
		got, ok := decodeBPTCPayload(layer2.BuildLCDataBurst(payload, elements.DataTypeCSBK, 0))
		if !ok || got != payload {
			t.Fatalf("payload %x: decoded %x ok=%v", payload, got, ok)
		}
	}
}

func voiceLCPacket(lc [12]byte, dataType elements.DataType) models.Packet {
	return models.Packet{
		Signature:   "DMRD",
		Src:         0x2F9BE5,
		Dst:         2,
		Repeater:    3001,
		GroupCall:   true,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dataType),
		StreamID:    0x1234,
		DMRData:     layer2.BuildLCDataBurst(lc, dataType, 0),
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

	headers := tr.TranslateToIPSC(voiceLCPacket(hlc, elements.DataTypeVoiceLCHeader))
	if len(headers) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(headers))
	}
	for _, out := range headers {
		if !bytes.Equal(out[38:50], header[38:50]) {
			t.Fatalf("header LC %x, want %x", out[38:50], header[38:50])
		}
	}
	out := tr.TranslateToIPSC(voiceLCPacket(tlc, elements.DataTypeTerminatorWithLC))
	if len(out) != 1 || !bytes.Equal(out[0][38:50], term[38:50]) {
		t.Fatalf("terminator LC %x, want %x", out[0][38:50], term[38:50])
	}
}

func TestTranslateToIPSCKeepsServiceOptions(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	// Emergency + priority 3 with FID 0x10: a synthesized LC would lose these.
	lc := lc12(t, "001083000002"+"2f9be5"+"000000")
	setFullLCParity(&lc, elements.DataTypeVoiceLCHeader)
	out := tr.TranslateToIPSC(voiceLCPacket(lc, elements.DataTypeVoiceLCHeader))
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
		for _, dt := range []elements.DataType{elements.DataTypeVoiceLCHeader, elements.DataTypeTerminatorWithLC} {
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
			if !fullLCParityOK(got, dt) {
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
	if !fullLCParityOK(lc, elements.DataTypeVoiceLCHeader) {
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
	if !ok || !fullLCParityOK(lc, elements.DataTypeTerminatorWithLC) {
		t.Fatalf("LC %x ok=%v has bad parity", lc, ok)
	}
}
