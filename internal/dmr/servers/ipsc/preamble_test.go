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
	"github.com/stretchr/testify/assert"
)

// TestEachCallHasItsOwnCallSeq sends a voice call and two data calls to a
// peer. An XPR 8400 keeps the call sequence number in octet 5 for every
// packet of a call and gives each call the next one (testdata/xpr8400.txt;
// ipsc2hbrp-rig cap/data34u.txt and cap/preamble.txt).
func TestEachCallHasItsOwnCallSeq(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	seqs := map[uint8]bool{}
	voice := make([]models.Packet, 0, 3)
	for i, v := range []struct {
		frame dmrconst.FrameType
		value uint
	}{{dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader)}, {dmrconst.FrameVoiceSync, 0}, {dmrconst.FrameDataSync, uint(enums.DataTypeTerminatorWithLC)}} {
		p := makeTestMMDVMPacket(true, false, v.frame, v.value)
		p.StreamID, p.Seq = 50, uint(i)
		if v.frame == dmrconst.FrameVoiceSync {
			p.DMRData = makeVoiceDMRData(true)
		}
		voice = append(voice, p)
	}
	calls := [][]models.Packet{voice, mmdvmHostSMS(t, 60, 0, "a"), mmdvmHostSMS(t, 70, 0, "b")}
	for n, call := range calls {
		var seq uint8
		for i, pkt := range call {
			for _, f := range tr.TranslateToIPSC(pkt) {
				u := decodeUserT(t, f)
				if i == 0 {
					seq = u.CallSeq
				}
				assert.Equal(t, seq, u.CallSeq, "call %d packet %d", n, i)
			}
		}
		assert.False(t, seqs[seq], "call %d has the call sequence number of an earlier call", n)
		seqs[seq] = true
	}
}
