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
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
)

func damaged(raw layer2.RawBurst) layer2.RawBurst {
	for i := range 13 {
		raw[i] ^= 0xA5
	}
	return raw
}

func terminatorPacket(raw layer2.RawBurst) models.Packet {
	p := codec2Packet(raw, 0)
	p.DTypeOrVSeq = uint(enums.DataTypeTerminatorWithLC)
	return p
}

// ipscLCs returns the LC of each header, terminator and burst E that the
// translator sends for pkts, in order.
func ipscLCs(t *testing.T, pkts []models.Packet) []pdu.LCBytes {
	t.Helper()
	tr := newTestTranslator(t)
	var lcs []pdu.LCBytes
	for _, pkt := range pkts {
		for _, p := range tr.TranslateToIPSC(pkt) {
			u, err := ipscwire.DecodeUser(p)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case !u.IsVoice():
				lcs = append(lcs, pdu.LCBytes(u.Data.Octets[:9]))
			case u.Voice.Layout == ipscwire.VoiceLayoutE:
				lcs = append(lcs, pdu.LCBytes(u.Voice.LC))
			}
		}
	}
	return lcs
}

// TestDamagedHeaderLCGivesWayToTheEmbeddedLC sends a call whose Voice LC
// Header does not decode. Once the embedded LC of bursts B to E decodes,
// burst E carries it, with the FID of the call.
func TestDamagedHeaderLCGivesWayToTheEmbeddedLC(t *testing.T) {
	t.Parallel()
	lc, bursts := codec2Bursts(t)
	pkts := []models.Packet{codec2Packet(damaged(bursts[0]), 0)}
	for i := 1; i < len(bursts); i++ {
		pkts = append(pkts, codec2Packet(bursts[i], i))
	}
	lcs := ipscLCs(t, pkts)
	if got := lcs[len(lcs)-1]; got != lc {
		t.Fatalf("last burst E: LC % X, want % X", got, lc)
	}
}

// TestDamagedTerminatorLCKeepsTheLCOfTheCall sends a call whose Terminator
// with LC does not decode. The terminator carries the LC of the call from
// its header, with its FID, not one built from the packet fields.
func TestDamagedTerminatorLCKeepsTheLCOfTheCall(t *testing.T) {
	t.Parallel()
	lc, bursts := codec2Bursts(t)
	term, err := layer2.BuildDataBurst(mustCodeword(t, lc, enums.DataTypeTerminatorWithLC), enums.DataTypeTerminatorWithLC, 1, layer2.SyncSourceMS)
	if err != nil {
		t.Fatal(err)
	}
	pkts := make([]models.Packet, 0, len(bursts)+1)
	for i, raw := range bursts {
		pkts = append(pkts, codec2Packet(raw, i))
	}
	pkts = append(pkts, terminatorPacket(damaged(term)))
	lcs := ipscLCs(t, pkts)
	if got := lcs[len(lcs)-1]; got != lc {
		t.Fatalf("terminator: LC % X, want % X", got, lc)
	}
}

func mustCodeword(t *testing.T, lc pdu.LCBytes, dt enums.DataType) [12]byte {
	t.Helper()
	cw, err := lc.Codeword(dt)
	if err != nil {
		t.Fatal(err)
	}
	return cw
}
