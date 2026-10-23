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
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
)

// codec2Bursts returns the Voice LC Header and two superframes of a Codec2
// group call from 100 to 200 as QRadioLink sends it: FID 0xC2 in the LC,
// and 2400 bit/s frames of 6 octets with 3 zero octets in each slot.
func codec2Bursts(t *testing.T) (pdu.LCBytes, []layer2.RawBurst) {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(true, 200, 100, layer3.ServiceOptions{IsOpenVoiceCallMode: true, PriorityLevel: 2})
	if err != nil {
		t.Fatal(err)
	}
	lc.SetFID(0xC2)
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: layer2.SyncSourceMS})
	if err != nil {
		t.Fatal(err)
	}
	slots := make([][9]byte, 12*layer2.VoiceSlotsPerBurst)
	for i := range slots {
		for j := range 6 {
			slots[i][j] = byte(i*7 + j*31 + 0x55)
		}
	}
	bursts, err := vt.AddSlots(slots...)
	if err != nil {
		t.Fatal(err)
	}
	return lc, append([]layer2.RawBurst{vt.Header()}, bursts...)
}

func codec2Packet(raw layer2.RawBurst, i int) models.Packet {
	p := models.Packet{Signature: string(dmrconst.CommandDMRD), Src: 100, Dst: 200, GroupCall: true, StreamID: 0xC2, DMRData: raw, BER: -1, RSSI: -1}
	if i == 0 {
		p.FrameType, p.DTypeOrVSeq = dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader)
		return p
	}
	p.FrameType, p.DTypeOrVSeq = dmrconst.FrameVoice, uint((i-1)%6) //nolint:gosec
	if p.DTypeOrVSeq == 0 {
		p.FrameType = dmrconst.FrameVoiceSync
	}
	return p
}

// TestLateEntryBurstEKeepsTheEmbeddedLC sends a call without its Voice LC
// Header, as to a peer that joins late. Burst E must carry the LC that the
// embedded LC of bursts B to E gives, with its FID and Service Options,
// not one built from the packet fields.
func TestLateEntryBurstEKeepsTheEmbeddedLC(t *testing.T) {
	t.Parallel()
	lc, bursts := codec2Bursts(t)
	tr := newTestTranslator(t)
	e := 0
	for i := 1; i < len(bursts); i++ {
		for _, p := range tr.TranslateToIPSC(codec2Packet(bursts[i], i)) {
			u, err := ipscwire.DecodeUser(p)
			if err != nil {
				t.Fatal(err)
			}
			if u.Voice.Layout != ipscwire.VoiceLayoutE {
				continue
			}
			e++
			if pdu.LCBytes(u.Voice.LC) != lc {
				t.Fatalf("burst E %d: LC % X, want % X", e, u.Voice.LC, lc)
			}
		}
	}
	if e != 2 {
		t.Fatalf("%d bursts E, want 2", e)
	}
}

// TestCodec2VoiceDoesNotFitIPSC shows that IPSC framing cannot carry the
// voice of a Codec2 call unchanged. IPSC carries each voice burst as three
// AMBE+2 frames of 49 bits without FEC, 147 bits for the 216 bits of the
// three slots, so the Codec2 bits that the AMBE+2 FEC covers do not come
// back. The LC, with its FID, does.
func TestCodec2VoiceDoesNotFitIPSC(t *testing.T) {
	t.Parallel()
	lc, bursts := codec2Bursts(t)
	tr := newTestTranslator(t)
	changed := 0
	for i, raw := range bursts {
		for _, p := range tr.TranslateToIPSC(codec2Packet(raw, i)) {
			u, err := ipscwire.DecodeUser(p)
			if err != nil {
				t.Fatal(err)
			}
			if !u.IsVoice() {
				if pdu.LCBytes(u.Data.Octets[:9]) != lc {
					t.Fatalf("header LC % X, want % X", u.Data.Octets[:9], lc)
				}
				continue
			}
			back, err := u.RawBurst(0, layer2.SyncSourceMS)
			if err != nil {
				t.Fatal(err)
			}
			if back.VoiceSlots() != raw.VoiceSlots() {
				changed++
			}
		}
	}
	if changed != len(bursts)-1 {
		t.Fatalf("%d of %d voice bursts changed on the IPSC wire, want all", changed, len(bursts)-1)
	}
}
