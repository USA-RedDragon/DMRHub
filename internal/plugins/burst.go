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

package plugins

import (
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

const (
	payloadLen = 33
	maxSeq     = 255
	maxDType   = 15
)

// ToBurst returns the protocol form of p.
func ToBurst(p *models.Packet) *pluginv1.Burst {
	slot := uint32(1)
	if p.Slot {
		slot = 2
	}
	return &pluginv1.Burst{
		Source: uint32(p.Src), Destination: uint32(p.Dst), GroupCall: p.GroupCall, Slot: slot, //nolint:gosec // DMR IDs are 24 bits
		StreamId: uint32(p.StreamID), FrameType: frameTypeOf(p.FrameType), //nolint:gosec // stream IDs are 32 bits
		DataTypeOrVoiceSequence: uint32(p.DTypeOrVSeq), Sequence: uint32(p.Seq), //nolint:gosec // 4 and 8 bits
		Payload: p.DMRData[:], Repeater: uint32(p.Repeater), //nolint:gosec // repeater IDs fit 32 bits
	}
}

func frameTypeOf(t dmrconst.FrameType) pluginv1.FrameType {
	switch t {
	case dmrconst.FrameVoice:
		return pluginv1.FrameType_FRAME_TYPE_VOICE
	case dmrconst.FrameVoiceSync:
		return pluginv1.FrameType_FRAME_TYPE_VOICE_SYNC
	case dmrconst.FrameDataSync:
		return pluginv1.FrameType_FRAME_TYPE_DATA_SYNC
	}
	return pluginv1.FrameType_FRAME_TYPE_UNSPECIFIED
}

// Owners tells which plugin owns a DMR ID.
type Owners interface {
	Owner(id uint) (uint, bool)
}

// FromBurst checks a burst that the plugin self, which owns ranges, sends,
// and returns it as a packet, or why the hub refuses it.
func FromBurst(b *pluginv1.Burst, self uint, ranges []models.PluginRange, owners Owners) (models.Packet, pluginv1.RefusalReason) {
	var p models.Packet
	if (b.GetSlot() != 1 && b.GetSlot() != 2) || len(b.GetPayload()) != payloadLen || b.GetStreamId() == 0 ||
		b.GetSequence() > maxSeq || b.GetDataTypeOrVoiceSequence() > maxDType || b.GetSource() > models.MaxDMRID || b.GetDestination() > models.MaxDMRID {
		return p, pluginv1.RefusalReason_REFUSAL_REASON_MALFORMED
	}
	switch b.GetFrameType() {
	case pluginv1.FrameType_FRAME_TYPE_VOICE:
		p.FrameType = dmrconst.FrameVoice
	case pluginv1.FrameType_FRAME_TYPE_VOICE_SYNC:
		p.FrameType = dmrconst.FrameVoiceSync
	case pluginv1.FrameType_FRAME_TYPE_DATA_SYNC:
		p.FrameType = dmrconst.FrameDataSync
	case pluginv1.FrameType_FRAME_TYPE_UNSPECIFIED:
		return p, pluginv1.RefusalReason_REFUSAL_REASON_MALFORMED
	default:
		return p, pluginv1.RefusalReason_REFUSAL_REASON_MALFORMED
	}
	src, dst := uint(b.GetSource()), uint(b.GetDestination())
	if !inRanges(ranges, src) {
		return p, pluginv1.RefusalReason_REFUSAL_REASON_SOURCE
	}
	if b.GetGroupCall() {
		if !inRanges(ranges, dst) {
			return p, pluginv1.RefusalReason_REFUSAL_REASON_DESTINATION
		}
	} else if _, owned := owners.Owner(dst); owned || dst == 0 || inRanges(ranges, dst) {
		return p, pluginv1.RefusalReason_REFUSAL_REASON_DESTINATION
	}
	p.Signature = string(dmrconst.CommandDMRD)
	p.Src, p.Dst, p.GroupCall, p.Slot = src, dst, b.GetGroupCall(), b.GetSlot() == 2
	p.StreamID, p.Seq, p.DTypeOrVSeq = uint(b.GetStreamId()), uint(b.GetSequence()), uint(b.GetDataTypeOrVoiceSequence())
	copy(p.DMRData[:], b.GetPayload())
	p.BER, p.RSSI = -1, -1
	return p, pluginv1.RefusalReason_REFUSAL_REASON_UNSPECIFIED
}

func inRanges(ranges []models.PluginRange, id uint) bool {
	for _, r := range ranges {
		if r.Contains(id) {
			return true
		}
	}
	return false
}
