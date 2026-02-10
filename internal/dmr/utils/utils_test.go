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

package utils_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"github.com/stretchr/testify/assert"
)

func TestCheckPacketTypeVoiceHeader(t *testing.T) {
	t.Parallel()
	packet := models.Packet{
		Src:         100,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead),
	}
	isVoice, isData := utils.CheckPacketType(packet)
	assert.True(t, isVoice, "voice header should be voice")
	assert.False(t, isData, "voice header should not be data")
}

func TestCheckPacketTypeVoiceTerminator(t *testing.T) {
	t.Parallel()
	packet := models.Packet{
		Src:         100,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceTerm),
	}
	isVoice, isData := utils.CheckPacketType(packet)
	assert.True(t, isVoice, "voice terminator should be voice")
	assert.False(t, isData, "voice terminator should not be data")
}

func TestCheckPacketTypeDataSync(t *testing.T) {
	t.Parallel()
	packet := models.Packet{
		Src:         100,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: 0x03, // A data type that is not voice head or voice term
	}
	isVoice, isData := utils.CheckPacketType(packet)
	assert.False(t, isVoice, "data sync should not be voice")
	assert.True(t, isData, "data sync should be data")
}

func TestCheckPacketTypeVoice(t *testing.T) {
	t.Parallel()
	packet := models.Packet{
		Src:       100,
		FrameType: dmrconst.FrameVoice,
	}
	isVoice, isData := utils.CheckPacketType(packet)
	assert.True(t, isVoice, "voice frame should be voice")
	assert.False(t, isData, "voice frame should not be data")
}

func TestCheckPacketTypeVoiceSync(t *testing.T) {
	t.Parallel()
	packet := models.Packet{
		Src:       100,
		FrameType: dmrconst.FrameVoiceSync,
	}
	isVoice, isData := utils.CheckPacketType(packet)
	assert.True(t, isVoice, "voice sync should be voice")
	assert.False(t, isData, "voice sync should not be data")
}

func TestCheckPacketTypeUnknownFrame(t *testing.T) {
	t.Parallel()
	packet := models.Packet{
		Src:       100,
		FrameType: 99,
	}
	isVoice, isData := utils.CheckPacketType(packet)
	assert.False(t, isVoice, "unknown frame should not be voice")
	assert.False(t, isData, "unknown frame should not be data")
}
