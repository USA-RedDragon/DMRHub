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

package hub

import (
	"bufio"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
)

// recordedParrotCall returns the call of DMRGateway in
// ../testdata/dmrgateway-master.txt (3191868 to talkgroup 91, one
// superframe) as the parrot records it: source and target swapped, as a
// private call.
func recordedParrotCall(t *testing.T) []models.Packet {
	t.Helper()
	f, err := os.Open("../testdata/dmrgateway-master.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []models.Packet
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) != 4 || fs[1] != "r>m" || !strings.HasPrefix(fs[3], "444d5244") {
			continue
		}
		p, err := hex.DecodeString(fs[3])
		if err != nil {
			t.Fatal(err)
		}
		pkt, ok := models.UnpackPacket(p)
		if !ok {
			t.Fatalf("%x does not decode", p)
		}
		pkt.Src, pkt.Dst, pkt.GroupCall = pkt.Dst, pkt.Src, false
		out = append(out, pkt)
	}
	return out
}

func TestParrotReplyNamesTheReply(t *testing.T) {
	t.Parallel()
	rec := recordedParrotCall(t)
	if len(rec) != 8 {
		t.Fatalf("capture has %d DMRD packets, want 8", len(rec))
	}
	reply := parrotReply(rec, 1)
	if len(reply) != 8 {
		t.Fatalf("reply has %d packets, want 8", len(reply))
	}
	for i, pkt := range reply {
		if pkt.Src != 91 || pkt.Dst != 3191868 || pkt.GroupCall || pkt.StreamID != rec[0].StreamID || pkt.Seq != uint(i) {
			t.Fatalf("packet %d: %s", i, pkt.String())
		}
	}
	for _, i := range []int{0, 7} {
		dt := enums.DataTypeVoiceLCHeader
		if i == 7 {
			dt = enums.DataTypeTerminatorWithLC
		}
		if reply[i].FrameType != dmrconst.FrameDataSync || reply[i].DTypeOrVSeq != uint(dt) {
			t.Fatalf("packet %d: frame %d value %d, want data type %s", i, reply[i].FrameType, reply[i].DTypeOrVSeq, dt)
		}
		cw, ok := layer2.RawBurst(reply[i].DMRData).MMDVMHostPayload(dt)
		if !ok {
			t.Fatalf("packet %d: Full LC does not decode", i)
		}
		lc := pdu.LCBytes(cw[:9])
		group, target, source, ok := lc.Call()
		if !ok || group || target != 3191868 || source != 91 {
			t.Fatalf("packet %d: LC %x, want a private call from 91 to 3191868", i, cw)
		}
	}
	for i := 1; i < 7; i++ {
		idx := i - 1
		want := dmrconst.FrameVoice
		if idx == 0 {
			want = dmrconst.FrameVoiceSync
		}
		if reply[i].FrameType != want || reply[i].DTypeOrVSeq != uint(idx) {
			t.Fatalf("packet %d: frame %d position %d", i, reply[i].FrameType, reply[i].DTypeOrVSeq)
		}
		got, rec := layer2.RawBurst(reply[i].DMRData).VoiceSlots(), layer2.RawBurst(rec[i].DMRData).VoiceSlots()
		if got != rec {
			t.Fatalf("packet %d: voice %x, want the recorded %x", i, got, rec)
		}
	}
}

func TestParrotReplyKeepsRecordingWithoutVoice(t *testing.T) {
	t.Parallel()
	rec := []models.Packet{{Signature: "DMRD", Src: 9990, Dst: 1000001, FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(enums.DataTypeCSBK), StreamID: 1}}
	if got := parrotReply(rec, 1); len(got) != 1 || !got[0].Equal(rec[0]) {
		t.Fatalf("got %v, want the recording", got)
	}
}

// recordedCodec2Call returns a Codec2 call from 3191868 to the parrot 9990
// (FID 0xC2, Service Options that are not zero, 2400 bit/s frames of 6
// octets and 3 zero octets in each slot) that ends after burst C of its
// third superframe, as the parrot records it, with its slots.
func recordedCodec2Call(t *testing.T) ([]models.Packet, pdu.LCBytes, [][9]byte) {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(false, 9990, 3191868, layer3.ServiceOptions{IsOpenVoiceCallMode: true, PriorityLevel: 2})
	if err != nil {
		t.Fatal(err)
	}
	lc.SetFID(0xC2)
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: layer2.SyncSourceMS})
	if err != nil {
		t.Fatal(err)
	}
	slots := make([][9]byte, 15*layer2.VoiceSlotsPerBurst)
	for i := range slots {
		for j := range 6 {
			slots[i][j] = byte(i*13 + j*29 + 1)
		}
	}
	bursts, err := vt.AddSlots(slots...)
	if err != nil {
		t.Fatal(err)
	}
	pkt := func(raw layer2.RawBurst, frame dmrconst.FrameType, value uint) models.Packet {
		return models.Packet{Signature: "DMRD", Src: 9990, Dst: 3191868, Repeater: 100001, FrameType: frame, DTypeOrVSeq: value, StreamID: 0xC2, DMRData: raw, BER: -1, RSSI: -1}
	}
	rec := make([]models.Packet, 0, len(bursts)+2)
	rec = append(rec, pkt(vt.Header(), dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader)))
	for i, raw := range bursts {
		frame := dmrconst.FrameVoice
		if i%6 == 0 {
			frame = dmrconst.FrameVoiceSync
		}
		rec = append(rec, pkt(raw, frame, uint(i%6)))
	}
	rec = append(rec, pkt(vt.Terminator(), dmrconst.FrameDataSync, uint(enums.DataTypeTerminatorWithLC)))
	for i := range rec {
		rec[i].Seq = uint(i)
	}
	return rec, lc, slots
}

// TestParrotReplyKeepsTheCodec2Call checks that the reply to a Codec2 call
// carries the LC of the caller, with its FID and Service Options and the
// addresses of the reply, and the recorded voice, and that the last
// superframe is padded with the last recorded slot, not with AMBE+2
// silence, which is noise to Codec2.
func TestParrotReplyKeepsTheCodec2Call(t *testing.T) {
	t.Parallel()
	rec, lc, slots := recordedCodec2Call(t)
	want := lc
	if err := want.SetCall(false, 3191868, 9990); err != nil {
		t.Fatal(err)
	}
	reply := parrotReply(rec, 1)
	if len(reply) != 1+18+1 {
		t.Fatalf("reply has %d packets, want 20", len(reply))
	}
	for _, i := range []int{0, len(reply) - 1} {
		dt := enums.DataType(reply[i].DTypeOrVSeq)
		cw, ok := layer2.RawBurst(reply[i].DMRData).MMDVMHostPayload(dt)
		if !ok {
			t.Fatalf("packet %d: Full LC does not decode", i)
		}
		if got := pdu.LCBytes(cw[:9]); got != want {
			t.Fatalf("packet %d (%s): LC % X, want % X", i, dt, got[:], want[:])
		}
	}
	for v := range 18 {
		got := layer2.RawBurst(reply[1+v].DMRData).VoiceSlots()
		for k, s := range got {
			j := v*layer2.VoiceSlotsPerBurst + k
			exp := slots[len(slots)-1]
			if j < len(slots) {
				exp = slots[j]
			}
			if s != exp {
				t.Fatalf("slot %d: % X, want % X", j, s, exp)
			}
		}
	}
	for sf := range 3 {
		var f [4][4]byte
		for k := range 4 {
			f[k] = layer2.RawBurst(reply[2+sf*6+k].DMRData).EmbeddedData()
		}
		if got, res := layer2.DecodeEmbeddedLC(f); res.Uncorrectable || pdu.LCBytes(got) != want {
			t.Fatalf("superframe %d: embedded LC % X, want % X", sf, got, want[:])
		}
	}
}
