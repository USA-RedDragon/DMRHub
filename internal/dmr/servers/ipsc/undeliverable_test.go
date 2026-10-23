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
	"slices"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
)

func badBurst(t *testing.T) layer2.RawBurst {
	t.Helper()
	for seed := range 256 {
		var burst layer2.RawBurst
		for i := range burst {
			burst[i] = byte(i*37 + 11 + seed*i*i)
		}
		_, r34 := burst.Rate34Payload()
		_, bptc := burst.BPTCPayload()
		if r34.Uncorrectable && bptc.Uncorrectable {
			return burst
		}
	}
	t.Fatal("no test burst fails both FECs")
	return layer2.RawBurst{}
}

type uncarried struct {
	name  string
	frame dmrconst.FrameType
	dt    enums.DataType
}

func uncarriedBursts() []uncarried {
	return []uncarried{
		{"idle", dmrconst.FrameDataSync, enums.DataTypeIdle},
		{"USBD", dmrconst.FrameDataSync, enums.DataTypeUnifiedSingleBlock},
		{"reserved", dmrconst.FrameDataSync, 13},
		{"bad CSBK", dmrconst.FrameDataSync, enums.DataTypeCSBK},
		{"bad data header", dmrconst.FrameDataSync, enums.DataTypeDataHeader},
		{"bad rate 1/2", dmrconst.FrameDataSync, enums.DataTypeRate12},
		{"bad rate 3/4", dmrconst.FrameDataSync, enums.DataTypeRate34},
	}
}

func uncarriedPacket(t *testing.T, c uncarried) models.Packet {
	t.Helper()
	return models.Packet{
		Signature:   "DMRD",
		Src:         3191868,
		Dst:         91,
		GroupCall:   true,
		FrameType:   c.frame,
		DTypeOrVSeq: uint(c.dt),
		StreamID:    0x5151,
		DMRData:     badBurst(t),
	}
}

func TestTranslateToIPSCReportsUndeliverable(t *testing.T) {
	t.Parallel()
	cases := append(uncarriedBursts(), uncarried{"frame type 3", 3, 0})
	for _, c := range cases {
		tr := newTestTranslator(t)
		var reasons []string
		tr.onUndeliverable = func(_ models.Packet, reason string) { reasons = append(reasons, reason) }
		if out := tr.TranslateToIPSC(uncarriedPacket(t, c)); len(out) != 0 {
			t.Errorf("%s: %d IPSC packets, want none until the rig test", c.name, len(out))
		}
		if len(reasons) != 1 {
			t.Errorf("%s: %d undeliverable reports, want 1", c.name, len(reasons))
		}
	}
}

func TestTranslateToIPSCSendsRate1AsRawBlocks(t *testing.T) {
	t.Parallel()
	for _, slot := range []bool{false, true} {
		tr := newTestTranslator(t)
		tr.onUndeliverable = func(_ models.Packet, reason string) { t.Errorf("rate 1 reported undeliverable: %s", reason) }
		pkt := uncarriedPacket(t, uncarried{"rate 1", dmrconst.FrameDataSync, enums.DataTypeRate1})
		pkt.Slot = slot
		out := tr.TranslateToIPSC(pkt)
		if len(out) != 1 {
			t.Fatalf("slot 2 %v: %d IPSC packets, want 1", slot, len(out))
		}
		u, err := ipscwire.DecodeUser(out[0])
		if err != nil {
			t.Fatal(err)
		}
		if u.Burst != ipscwire.BurstRaw || u.Data.Bits != 196 {
			t.Fatalf("burst %v with %d bits, want a raw block", u.Burst, u.Data.Bits)
		}
		if got, want := [25]byte(u.Data.Octets[:25]), layer2.RawBurst(pkt.DMRData).Info(); got != want {
			t.Fatalf("info bits %x, want %x", got, want)
		}
		if st, ok := u.Data.SlotType(); !ok || st.DataType() != enums.DataTypeRate1 {
			t.Fatalf("slot type %v %v, want rate 1", st, ok)
		}
		if got, want := u.Info.Slot() == enums.Slot2, slot; got != want {
			t.Fatalf("slot 2 %v, want %v", got, want)
		}
		back := tr.TranslateToMMDVM(out[0][0], out[0])
		if len(back) != 1 || back[0].DTypeOrVSeq != uint(enums.DataTypeRate1) || layer2.RawBurst(back[0].DMRData).Info() != layer2.RawBurst(pkt.DMRData).Info() {
			t.Fatalf("the raw block did not come back as the rate 1 block")
		}
	}
}

func TestTranslateToIPSCRawBlocks(t *testing.T) {
	t.Parallel()
	for _, c := range uncarriedBursts() {
		tr := newTestTranslator(t)
		tr.rawBlocksAnXPRDrops = true
		tr.onUndeliverable = func(_ models.Packet, reason string) { t.Errorf("%s: reported undeliverable: %s", c.name, reason) }
		pkt := uncarriedPacket(t, c)
		out := tr.TranslateToIPSC(pkt)
		if len(out) != 1 {
			t.Fatalf("%s: %d IPSC packets, want 1", c.name, len(out))
		}
		u, err := ipscwire.DecodeUser(out[0])
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if u.Burst != ipscwire.BurstRaw || u.Data.Bits != 196 {
			t.Fatalf("%s: burst %v with %d bits, want a raw block", c.name, u.Burst, u.Data.Bits)
		}
		info := layer2.RawBurst(pkt.DMRData).Info()
		got := [25]byte(u.Data.Octets[:25])
		if got != info {
			t.Fatalf("%s: info bits %x, want %x", c.name, got, info)
		}
		if st, ok := u.Data.SlotType(); !ok || st.DataType() != c.dt {
			t.Fatalf("%s: slot type %v %v, want data type %v", c.name, st, ok, c.dt)
		}
	}
}

func TestCleanupKeepsATranslatorInUse(t *testing.T) {
	t.Parallel()
	s := &IPSCServer{localID: 12345}
	first := s.egressTranslator(200001)
	s.cleanupStaleStreams()
	if s.egressTranslator(200001) != first {
		t.Fatal("cleanup dropped the translator of a peer that was just given it")
	}
}

func reportsOf(tr *IPSCTranslator) *[]string {
	var reasons []string
	tr.onUndeliverable = func(_ models.Packet, reason string) { reasons = append(reasons, reason) }
	return &reasons
}

func TestTranslateToIPSCReportsADataBurstInAVoiceStream(t *testing.T) {
	t.Parallel()
	var c pdu.CSBK
	c.LastBlock = true
	if err := c.SetMotorolaCallAlert(pdu.MotorolaCallAlertPDU{TargetAddress: 91, SourceAddress: 3191868}); err != nil {
		t.Fatal(err)
	}
	raw, err := layer2.BuildBurst(&c, 1, layer2.SyncSourceMS)
	if err != nil {
		t.Fatal(err)
	}
	tr := newTestTranslator(t)
	reasons := reportsOf(tr)
	pkt := uncarriedPacket(t, uncarried{"voice", dmrconst.FrameVoice, 1})
	pkt.DMRData = raw
	if out := tr.TranslateToIPSC(pkt); len(out) != 0 {
		t.Fatalf("%d IPSC packets, want none", len(out))
	}
	if len(*reasons) != 1 || (*reasons)[0] != reasonDataInVoice {
		t.Fatalf("reports %v, want [%s]", *reasons, reasonDataInVoice)
	}
}

func TestBurstsThatDoNotBuildAreReported(t *testing.T) {
	t.Parallel()
	pkt := uncarriedPacket(t, uncarried{"data", dmrconst.FrameDataSync, enums.DataTypeCSBK})

	tr := newTestTranslator(t)
	reasons := reportsOf(tr)
	if out := tr.buildIPSCDataPacket(pkt, &streamState{}, enums.DataTypeRate1); out != nil {
		t.Fatal("SetRawBurst took a rate 1 burst")
	}
	if out := tr.buildVoiceLC(pkt, &streamState{}, enums.DataTypeRate34, false); out != nil {
		t.Fatal("BuildDataBurst took a rate 3/4 LC")
	}
	if out := tr.encodeUser(pkt, &ipscwire.User{}); out != nil {
		t.Fatal("AppendBinary took a packet without an opcode")
	}
	if want := []string{reasonBuild, reasonBuild, reasonEncode}; !slices.Equal(*reasons, want) {
		t.Fatalf("reports %v, want %v", *reasons, want)
	}
}
