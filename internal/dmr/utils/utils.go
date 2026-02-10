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
	"log/slog"

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
			slog.Debug("Voice terminator packet received", "src", packet.Src)
		case dmrconst.DTypeVoiceHead:
			isVoice = true
			slog.Debug("Voice header packet received", "src", packet.Src)
		default:
			isData = true
			slog.Debug("Data packet received", "src", packet.Src, "dtype", packet.DTypeOrVSeq)
		}
	case dmrconst.FrameVoice:
		isVoice = true
		slog.Debug("Voice packet received", "src", packet.Src, "vseq", packet.DTypeOrVSeq)
	case dmrconst.FrameVoiceSync:
		isVoice = true
		slog.Debug("Voice sync packet received", "src", packet.Src, "dtype", packet.DTypeOrVSeq)
	}
	return isVoice, isData
}
