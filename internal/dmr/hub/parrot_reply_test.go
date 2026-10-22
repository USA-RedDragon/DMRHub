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
