// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
)

func CheckPacketType(packet models.Packet) (bool, bool) {
	isVoice := false
	isData := false
	switch packet.FrameType {
	case dmrconst.FrameDataSync:
		switch dmrconst.DataType(packet.DTypeOrVSeq) {
		case dmrconst.DTypeVoiceTerm:
			isVoice = true
			if config.GetConfig().Debug {
				logging.GetLogger(logging.Access).Logf(CheckPacketType, "Voice terminator from %d", packet.Src)
			}
		case dmrconst.DTypeVoiceHead:
			isVoice = true
			if config.GetConfig().Debug {
				logging.GetLogger(logging.Access).Logf(CheckPacketType, "Voice header from %d", packet.Src)
			}
		default:
			isData = true
			if config.GetConfig().Debug {
				logging.GetLogger(logging.Access).Logf(CheckPacketType, "Data packet from %d, dtype: %d", packet.Src, packet.DTypeOrVSeq)
			}
		}
	case dmrconst.FrameVoice:
		isVoice = true
		if config.GetConfig().Debug {
			logging.GetLogger(logging.Access).Logf(CheckPacketType, "Voice packet from %d, vseq %d", packet.Src, packet.DTypeOrVSeq)
		}
	case dmrconst.FrameVoiceSync:
		isVoice = true
		if config.GetConfig().Debug {
			logging.GetLogger(logging.Access).Logf(CheckPacketType, "Voice sync packet from %d, dtype: %d", packet.Src, packet.DTypeOrVSeq)
		}
	}
	return isVoice, isData
}
