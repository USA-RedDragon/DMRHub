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

package utils

import (
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
)

func CheckPacketType(packet models.Packet) (bool, bool) {
	isVoice := false
	isData := false
	switch packet.FrameType {
	case dmrconst.FrameDataSync:
		switch dmrconst.DataType(packet.DTypeOrVSeq) {
		case dmrconst.DTypeVoiceTerm:
			isVoice = true
		case dmrconst.DTypeVoiceHead:
			isVoice = true
		default:
			isData = true
		}
	case dmrconst.FrameVoice:
		isVoice = true
	case dmrconst.FrameVoiceSync:
		isVoice = true
	}
	return isVoice, isData
}
