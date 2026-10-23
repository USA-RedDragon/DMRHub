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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
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
		{"rate 1", dmrconst.FrameDataSync, enums.DataTypeRate1},
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

func TestTranslateToIPSCRawBlocks(t *testing.T) {
	t.Parallel()
	for _, c := range uncarriedBursts() {
		tr := newTestTranslator(t)
		tr.rawBlocks = true
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
