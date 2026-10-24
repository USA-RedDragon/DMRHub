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

package plugin

import (
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
)

// DataOf returns burst as a dmrgo DMRD packet, whose Hint tells the kind of
// the burst to dmrgo's voice and data receivers.
func DataOf(b *pluginv1.Burst) dmrd.Data {
	d := dmrd.Data{
		Seq:         uint8(b.GetSequence()), //nolint:gosec // 8 bits
		Source:      b.GetSource(),
		Destination: b.GetDestination(),
		Repeater:    b.GetRepeater(),
		Slot:        enums.Slot1,
		Private:     !b.GetGroupCall(),
		Value:       uint8(b.GetDataTypeOrVoiceSequence()), //nolint:gosec // 4 bits
		Stream:      b.GetStreamId(),
	}
	if b.GetSlot() == 2 {
		d.Slot = enums.Slot2
	}
	switch b.GetFrameType() {
	case pluginv1.FrameType_FRAME_TYPE_VOICE_SYNC:
		d.Frame = dmrd.FrameVoiceSync
	case pluginv1.FrameType_FRAME_TYPE_DATA_SYNC:
		d.Frame = dmrd.FrameDataSync
	case pluginv1.FrameType_FRAME_TYPE_VOICE, pluginv1.FrameType_FRAME_TYPE_UNSPECIFIED:
		d.Frame = dmrd.FrameVoice
	}
	copy(d.Burst[:], b.GetPayload())
	return d
}

// BurstOf returns the DMRD packet d as a burst.
func BurstOf(d *dmrd.Data) *pluginv1.Burst {
	b := &pluginv1.Burst{
		Source: d.Source, Destination: d.Destination, GroupCall: !d.Private, Slot: 1, StreamId: d.Stream,
		DataTypeOrVoiceSequence: uint32(d.Value), Sequence: uint32(d.Seq), Payload: append([]byte(nil), d.Burst[:]...),
		Repeater: d.Repeater, FrameType: pluginv1.FrameType_FRAME_TYPE_VOICE,
	}
	if d.Slot == enums.Slot2 {
		b.Slot = 2
	}
	switch d.Frame {
	case dmrd.FrameVoiceSync:
		b.FrameType = pluginv1.FrameType_FRAME_TYPE_VOICE_SYNC
	case dmrd.FrameDataSync, dmrd.FrameReserved:
		b.FrameType = pluginv1.FrameType_FRAME_TYPE_DATA_SYNC
	case dmrd.FrameVoice:
	}
	return b
}

// IsTerminator reports whether b ends a voice call: a data sync burst with
// the Terminator with LC data type.
func IsTerminator(b *pluginv1.Burst) bool {
	return b.GetFrameType() == pluginv1.FrameType_FRAME_TYPE_DATA_SYNC &&
		b.GetDataTypeOrVoiceSequence() == uint32(enums.DataTypeTerminatorWithLC)
}

// IsVoice reports whether b is a burst of a voice call: a voice burst, a
// Voice LC Header or a Terminator with LC.
func IsVoice(b *pluginv1.Burst) bool {
	switch b.GetFrameType() {
	case pluginv1.FrameType_FRAME_TYPE_VOICE, pluginv1.FrameType_FRAME_TYPE_VOICE_SYNC:
		return true
	case pluginv1.FrameType_FRAME_TYPE_DATA_SYNC:
		t := b.GetDataTypeOrVoiceSequence()
		return t == uint32(enums.DataTypeVoiceLCHeader) || t == uint32(enums.DataTypeTerminatorWithLC)
	case pluginv1.FrameType_FRAME_TYPE_UNSPECIFIED:
	}
	return false
}
