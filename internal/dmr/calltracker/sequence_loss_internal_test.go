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

package calltracker

import (
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/stretchr/testify/assert"
)

func voiceBursts(call *models.Call, seqs ...uint) {
	for _, seq := range seqs {
		ft := dmrconst.FrameVoice
		if seq == dmrconst.VoiceA {
			ft = dmrconst.FrameVoiceSync
		}
		calcSequenceLoss(call, models.Packet{FrameType: ft, DTypeOrVSeq: seq})
	}
}

func TestASyncBurstAfterLostBurstsCountsThem(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		for last := uint(dmrconst.VoiceB); last < dmrconst.VoiceF; last++ {
			var call models.Call
			calcSequenceLoss(&call, models.Packet{FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead)})
			for seq := uint(dmrconst.VoiceA); seq <= last; seq++ {
				voiceBursts(&call, seq)
			}
			voiceBursts(&call, dmrconst.VoiceA)

			lost := dmrconst.VoiceF - last
			assert.Equal(t, lost, call.LostSequences, "last burst %d", last)
			assert.Equal(t, 1+(last+1)+lost+1, call.TotalPackets, "last burst %d", last)
			assert.Equal(t, uint(dmrconst.VoiceA), call.LastFrameNum)
		}
	})
}

func TestAWholeSuperframeCountsNoLoss(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var call models.Call
		calcSequenceLoss(&call, models.Packet{FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead)})
		voiceBursts(&call, 0, 1, 2, 3, 4, 5, 0, 1, 2, 3, 4, 5)
		calcSequenceLoss(&call, models.Packet{FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(dmrconst.DTypeVoiceTerm)})
		assert.Equal(t, uint(0), call.LostSequences)
		assert.Equal(t, uint(14), call.TotalPackets)
	})
}

func TestARepeatedOrBadBurstNeverWrapsTheCounts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var call models.Call
		calcSequenceLoss(&call, models.Packet{FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead)})
		voiceBursts(&call, 0, 1, 2, 2)
		calcSequenceLoss(&call, models.Packet{FrameType: dmrconst.FrameVoice, DTypeOrVSeq: 9})
		calcSequenceLoss(&call, models.Packet{FrameType: dmrconst.FrameVoiceSync, DTypeOrVSeq: dmrconst.VoiceA})
		assert.Less(t, call.LostSequences, uint(1<<16))
		assert.Less(t, call.TotalPackets, uint(1<<16))
	})
}
