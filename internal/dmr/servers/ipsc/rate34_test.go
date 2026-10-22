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

// An XPR 8400 relayed a confirmed rate 3/4 block ("DMRGO TEST", DBSN 0)
// from 3191868 to 3191868 as this IPSC packet. Octets 38-55 hold the 18
// decoded octets rotated left by 2, so the DBSN and CRC-9 come last.
const captureRate34 = "840004ce2eff30b43c30b43c0100007a9e40805ebcf232c2c0b0000000000840000d800a0090" +
	"444d52474f20544553540000a3ecda7c002e" + "007846d5"

func TestTranslateToMMDVMRate34Capture(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	data := mustHex(t, captureRate34)
	out := tr.TranslateToMMDVM(data[0], data)
	if len(out) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(out))
	}
	if out[0].FrameType != dmrconst.FrameDataSync || out[0].DTypeOrVSeq != uint(enums.DataTypeRate34) {
		t.Fatalf("frame type %d dtype %d", out[0].FrameType, out[0].DTypeOrVSeq)
	}
	raw := layer2.RawBurst(out[0].DMRData)
	if dt := raw.SlotType().DataType; dt != enums.DataTypeRate34 {
		t.Fatalf("slot type data type %s", dt)
	}
	p, res := raw.Rate34Payload()
	if res.Uncorrectable || res.ErrorsCorrected != 0 {
		t.Fatalf("rate 3/4 decode %+v", res)
	}
	if got := hex.EncodeToString(p[:]); got != "002e444d52474f20544553540000a3ecda7c" {
		t.Fatalf("rate 3/4 payload %s", got)
	}
	block := pdu.Rate34Data{Data: p}
	if !block.ConfirmedCRC9OK() || block.ConfirmedDBSN() != 0 {
		t.Fatalf("confirmed block CRC-9 ok=%v DBSN %d", block.ConfirmedCRC9OK(), block.ConfirmedDBSN())
	}
}

func rate34Packet(burst [33]byte) models.Packet {
	return models.Packet{
		Signature:   "DMRD",
		Src:         3191868,
		Dst:         3191868,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(enums.DataTypeRate34),
		StreamID:    0x3434,
		DMRData:     burst,
	}
}

func TestTranslateToIPSCRate34(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	capture := mustHex(t, captureRate34)
	var block [18]byte
	copy(block[:], mustHex(t, "002e444d52474f20544553540000a3ecda7c"))
	burst, err := layer2.BuildRate34DataBurst(block, 7, layer2.SyncSourceMS)
	if err != nil {
		t.Fatal(err)
	}
	out := tr.TranslateToIPSC(rate34Packet(burst))
	if len(out) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(out))
	}
	got := out[0]
	if len(got) != len(capture) {
		t.Fatalf("packet length %d, want %d", len(got), len(capture))
	}
	if got[0] != 0x84 || got[30] != ipscBurstRate34 || !bytes.Equal(got[32:34], capture[32:34]) ||
		!bytes.Equal(got[36:56], capture[36:56]) {
		t.Fatalf("IPSC packet %x, want the layout of %x", got, capture)
	}

	back := tr.TranslateToMMDVM(got[0], got)
	if len(back) != 1 {
		t.Fatalf("expected 1 MMDVM packet, got %d", len(back))
	}
	if p, _ := layer2.RawBurst(back[0].DMRData).Rate34Payload(); p != block {
		t.Fatalf("round trip payload %x, want %x", p, block)
	}
}

func TestTranslateToIPSCRate34Undecodable(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	var burst layer2.RawBurst
	for i := range burst {
		burst[i] = byte(i*37 + 11)
	}
	if _, res := burst.Rate34Payload(); !res.Uncorrectable {
		t.Fatal("test burst decodes; pick another")
	}
	out := tr.TranslateToIPSC(rate34Packet(burst))
	if len(out) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(out))
	}
	got := out[0]
	if len(got) != 68 || got[30] != ipscBurstRaw || hex.EncodeToString(got[32:38]) != "0011800a00c4" {
		t.Fatalf("IPSC packet %x", got)
	}
	info := burst.Info()
	if !bytes.Equal(got[38:63], info[:]) || got[63] != 0 {
		t.Fatalf("info bits %x, want %x", got[38:64], info)
	}
	if back := tr.TranslateToMMDVM(got[0], got); len(back) != 0 {
		t.Fatalf("undecodable block translated to %d MMDVM packets", len(back))
	}
}

func TestInfoBits(t *testing.T) {
	t.Parallel()
	var burst layer2.RawBurst
	burst[0] = 0x80   // burst bit 0, info bit 0
	burst[12] = 0x40  // burst bit 97, info bit 97
	burst[20] = 0x20  // burst bit 162, in the slot type
	burst[20] |= 0x02 // burst bit 166, info bit 98
	burst[32] = 0x01  // burst bit 263, info bit 195
	got := burst.Info()
	var want [25]byte
	want[0] = 0x80
	want[12] = 0x40 | 0x20
	want[24] = 0x10
	if got != want {
		t.Fatalf("info bits %x, want %x", got, want)
	}
}
