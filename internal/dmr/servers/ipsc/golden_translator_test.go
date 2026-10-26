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
	"bufio"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
)

// readXPRCapture returns the packets of testdata/xpr8400.txt with the tag
// tag. The file holds the IPSC packets that a Motorola XPR 8400 sent,
// without their HMAC trailer (dmrgo transport/ipsc testdata, 2026-10-15 to
// 2026-10-20). Each "voicecall" and "datacall" call is in the file twice;
// this returns the first.
func readXPRCapture(t *testing.T, tag string) [][]byte {
	t.Helper()
	f, err := os.Open("testdata/xpr8400.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out [][]byte
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) != 2 || fs[0] != tag {
			continue
		}
		p, err := hex.DecodeString(fs[1])
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > 0 && hex.EncodeToString(p) == hex.EncodeToString(out[0]) {
			break
		}
		out = append(out, p)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func decodeUserT(t *testing.T, p []byte) ipscwire.User {
	t.Helper()
	u, err := ipscwire.DecodeUser(p)
	if err != nil {
		t.Fatalf("%x: %v", p, err)
	}
	return u
}

// TestXPRVoiceCallToMMDVM replays a group voice call of an XPR 8400 (3191868
// to talkgroup 9 on slot 2, color code 7) into the translator. The MMDVM
// packets must carry each burst in its superframe position, with the AMBE
// frames, the EMB (color code and PI) and the embedded data of the IPSC
// packet.
func TestXPRVoiceCallToMMDVM(t *testing.T) {
	t.Parallel()
	call := readXPRCapture(t, "voicecall")
	if len(call) != 20 {
		t.Fatalf("capture has %d packets, want 20", len(call))
	}
	tr := newTestTranslator(t)
	var out []models.Packet
	var in []ipscwire.User
	for _, p := range call {
		got := tr.TranslateToMMDVM(p[0], p)
		for range got {
			in = append(in, decodeUserT(t, p))
		}
		out = append(out, got...)
	}
	if len(out) != 20 {
		t.Fatalf("got %d MMDVM packets, want 20", len(out))
	}
	if out[0].FrameType != dmrconst.FrameDataSync || out[0].DTypeOrVSeq != uint(enums.DataTypeVoiceLCHeader) {
		t.Fatalf("first packet frame %d value %d, want a voice LC header", out[0].FrameType, out[0].DTypeOrVSeq)
	}
	last := out[len(out)-1]
	if last.FrameType != dmrconst.FrameDataSync || last.DTypeOrVSeq != uint(enums.DataTypeTerminatorWithLC) {
		t.Fatalf("last packet frame %d value %d, want a terminator", last.FrameType, last.DTypeOrVSeq)
	}
	for i, pkt := range out {
		if pkt.Src != 3191868 || pkt.Dst != 9 || !pkt.GroupCall || !pkt.Slot || pkt.Seq != uint(i) {
			t.Fatalf("packet %d: %s", i, pkt.String())
		}
	}
	for i := 1; i < len(out)-1; i++ {
		pkt, u := out[i], in[i]
		idx := (i - 1) % 6
		wantFrame := dmrconst.FrameVoice
		if idx == 0 {
			wantFrame = dmrconst.FrameVoiceSync
		}
		if pkt.FrameType != wantFrame || pkt.DTypeOrVSeq != uint(idx) {
			t.Fatalf("voice packet %d: frame %d position %d, want %d %d", i, pkt.FrameType, pkt.DTypeOrVSeq, wantFrame, idx)
		}
		raw := layer2.RawBurst(pkt.DMRData)
		frames, _ := raw.AMBEFrames()
		ambe, err := vocoder.PackAMBEVoice(frames)
		if err != nil || ambe != u.Voice.AMBE {
			t.Fatalf("voice packet %d: AMBE %x, want %x (%v)", i, ambe, u.Voice.AMBE, err)
		}
		if idx == 0 {
			continue
		}
		emb := raw.EMB()
		info, err := emb.Info()
		if err != nil || info != u.Voice.EMB {
			t.Fatalf("voice packet %d: EMB %02x, want %02x (%v)", i, info, u.Voice.EMB, err)
		}
		if emb.ColorCode != 7 {
			t.Fatalf("voice packet %d: color code %d, want 7", i, emb.ColorCode)
		}
		if raw.EmbeddedData() != u.Voice.Embedded {
			t.Fatalf("voice packet %d: embedded data %x, want %x", i, raw.EmbeddedData(), u.Voice.Embedded)
		}
	}
}

// TestXPRVoiceCallRoundTrip sends the MMDVM packets of the XPR 8400 voice
// call back to IPSC. Each IPSC packet must carry what the repeater sent:
// the opcode, call fields, burst type, voice layout, AMBE frames, embedded
// data, EMB and the Full LC of burst E and of the header and terminator.
func TestXPRVoiceCallRoundTrip(t *testing.T) {
	t.Parallel()
	call := readXPRCapture(t, "voicecall")
	rx := newTestTranslator(t)
	tx := newTestTranslator(t)
	var sent [][]byte
	for _, p := range call {
		for _, pkt := range rx.TranslateToMMDVM(p[0], p) {
			out := tx.TranslateToIPSC(pkt)
			if pkt.DTypeOrVSeq == uint(enums.DataTypeVoiceLCHeader) && pkt.FrameType == dmrconst.FrameDataSync {
				out = out[:1] // DMRHub repeats the header 3 times; the capture has one
			}
			sent = append(sent, out...)
		}
	}
	if len(sent) != len(call) {
		t.Fatalf("got %d IPSC packets, want %d", len(sent), len(call))
	}
	for i := range call {
		want, got := decodeUserT(t, call[i]), decodeUserT(t, sent[i])
		if got.Opcode != want.Opcode || got.Source != want.Source || got.Destination != want.Destination ||
			got.Priority != want.Priority || got.Info != want.Info || got.Burst != want.Burst ||
			got.RTP.PayloadType != want.RTP.PayloadType {
			t.Fatalf("packet %d: header %+v, want %+v", i, got, want)
		}
		if want.IsVoice() {
			if got.Voice != want.Voice {
				t.Fatalf("packet %d: voice %+v, want %+v", i, got.Voice, want.Voice)
			}
			continue
		}
		if got.Data.Octets != want.Data.Octets || got.Data.Bits != want.Data.Bits {
			t.Fatalf("packet %d: payload %x, want %x", i, got.Data.Octets, want.Data.Octets)
		}
	}
}

// TestXPRDataCallRoundTrip replays a confirmed rate 3/4 data call of an XPR
// 8400 (5 preamble CSBKs, a data header and 15 blocks) through both
// directions of the translator. Every block must reach MMDVM, and come back
// with the payload the repeater sent, in one call with one call control and
// call sequence number, and with the end flag and RTP payload type 0x5E on
// the last block only, as the repeater sent them.
func TestXPRDataCallRoundTrip(t *testing.T) {
	t.Parallel()
	call := readXPRCapture(t, "datacall")
	if len(call) != 21 {
		t.Fatalf("capture has %d packets, want 21", len(call))
	}
	rx := newTestTranslator(t)
	tx := newTestTranslator(t)
	var first ipscwire.User
	for i, p := range call {
		want := decodeUserT(t, p)
		pkts := rx.TranslateToMMDVM(p[0], p)
		if len(pkts) != 1 {
			t.Fatalf("packet %d: %d MMDVM packets, want 1", i, len(pkts))
		}
		if pkts[0].FrameType != dmrconst.FrameDataSync || pkts[0].DTypeOrVSeq != uint(want.Burst) || pkts[0].GroupCall {
			t.Fatalf("packet %d: %s", i, pkts[0].String())
		}
		out := tx.TranslateToIPSC(pkts[0])
		if len(out) != 1 {
			t.Fatalf("packet %d: %d IPSC packets, want 1", i, len(out))
		}
		got := decodeUserT(t, out[0])
		if got.Opcode != want.Opcode || got.Burst != want.Burst || got.Data.Bits != want.Data.Bits ||
			got.Data.Octets != want.Data.Octets || got.Priority != want.Priority {
			t.Fatalf("packet %d: %x, want %x", i, out[0], p)
		}
		if st, ok := got.Data.SlotType(); !ok || st.ColorCode() != 7 || st.DataType() != enums.DataType(want.Burst) {
			t.Fatalf("packet %d: slot type %v %v", i, st, ok)
		}
		if got.Info != want.Info || got.RTP.PayloadType != want.RTP.PayloadType {
			t.Fatalf("packet %d: call info %02x payload type %02x, want %02x %02x", i, got.Info, got.RTP.PayloadType, want.Info, want.RTP.PayloadType)
		}
		if i == 0 {
			first = got
		}
		if got.CallControl != first.CallControl || got.CallSeq != first.CallSeq {
			t.Fatalf("packet %d: call control %x call sequence %d, want %x %d", i, got.CallControl, got.CallSeq, first.CallControl, first.CallSeq)
		}
	}
}

// TestXPRRawBlocksReachMMDVM translates the undecodable blocks (burst type
// 0x1F) that the XPR 8400 sent. Each must reach MMDVM as one data burst with
// the data type of its slot type and the 196 information bits it carried.
func TestXPRRawBlocksReachMMDVM(t *testing.T) {
	t.Parallel()
	n := 0
	for _, p := range readXPRCapture(t, "one") {
		if len(p) < 31 || p[30] != byte(ipscwire.BurstRaw) {
			continue
		}
		n++
		u := decodeUserT(t, p)
		st, ok := u.Data.SlotType()
		if !ok {
			t.Fatalf("%x: no slot type", p)
		}
		pkts := newTestTranslator(t).TranslateToMMDVM(p[0], p)
		if len(pkts) != 1 {
			t.Fatalf("%x: %d MMDVM packets, want 1", p, len(pkts))
		}
		if pkts[0].FrameType != dmrconst.FrameDataSync || pkts[0].DTypeOrVSeq != uint(st.DataType()) {
			t.Fatalf("%x: %s, want data type %v", p, pkts[0].String(), st.DataType())
		}
		info := layer2.RawBurst(pkts[0].DMRData).Info()
		want := [25]byte(u.Data.Octets[:25])
		want[24] &= 0xF0
		if info != want {
			t.Fatalf("%x: info bits %x, want %x", p, info, want)
		}
	}
	if n == 0 {
		t.Fatal("the capture has no undecodable blocks")
	}
}
