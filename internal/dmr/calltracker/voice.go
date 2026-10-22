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
	"fmt"
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/mitchellh/hashstructure/v2"
)

type callMapStruct struct {
	Active        bool
	StreamID      uint
	UserID        uint
	DestinationID uint
	TimeSlot      bool
	GroupCall     bool
}

const unlinkID = 4000

type voiceKey uint64

func voiceKeyOf(packet *models.Packet) (voiceKey, error) {
	v := callMapStruct{
		Active:        true,
		StreamID:      packet.StreamID,
		UserID:        packet.Src,
		DestinationID: packet.Dst,
		TimeSlot:      packet.Slot,
		GroupCall:     packet.GroupCall,
	}
	hash, err := hashstructure.Hash(v, hashstructure.FormatV2, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to hash call from packet: %w", err)
	}
	return voiceKey(hash), nil
}

func (s *shard) voice(t *task, ch *channel, h *layer2.Hint, encrypted bool) {
	p := &t.p
	if p.Dst == unlinkID {
		return
	}
	if ch.rx != nil && ch.rx.State() != data.StateIdle {
		s.feed(t, ch, h, nil)
	}
	key, err := voiceKeyOf(p)
	if err != nil {
		slog.Error("CallTracker: error hashing call", "error", err)
		return
	}
	r, ok := s.rows[key]
	if !ok {
		r = s.open(t, ch, key, models.CallKindVoice, "", voiceHang)
		if r == nil {
			return
		}
		r.call.LastFrameNum = dmrconst.VoiceA
		s.t.remember(s, r)
	}
	if encrypted {
		r.call.SubKind = "encrypted"
	}
	s.updateVoice(r, t)
	if p.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(p.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
		s.finish(r, t.at)
	}
}

func (s *shard) updateVoice(r *row, t *task) {
	call := r.call
	packet := t.p
	r.last = t.at
	s.streams[packet.StreamID] = r
	if call.LastSeq == packet.Seq {
		return
	}
	elapsed := t.at.Sub(call.LastPacketTime)
	s.touch(r, t)
	call.LastSeq = packet.Seq
	call.Jitter = (call.Jitter + float32(elapsed.Milliseconds()-packetTimingMs)) / 2
	call.Duration = t.at.Sub(call.StartTime)

	lastTotalPackets := call.TotalPackets
	lastLostSequences := call.LostSequences
	calcSequenceLoss(call, packet)

	lastLoss := call.Loss
	call.Loss = float32(call.LostSequences) / float32(call.TotalPackets)
	if call.Loss > 1 {
		call.Loss = lastLoss
		call.TotalPackets = lastTotalPackets
		call.LostSequences = lastLostSequences
	}

	call.TotalBits += 141
	if packet.BER > 0 {
		call.TotalErrors += packet.BER
	}
	call.BER = float32(call.TotalErrors) / float32(call.TotalBits)
	call.Active = true
	if packet.RSSI > 0 {
		call.RSSI = (call.RSSI + float32(packet.RSSI)) / 2
	}
	s.t.publishCall(call)
}

func calcSequenceLoss(call *models.Call, packet models.Packet) {
	switch packet.FrameType {
	case dmrconst.FrameDataSync:
		switch packet.DTypeOrVSeq {
		case uint(dmrconst.DTypeVoiceHead):
			call.HasHeader = true
			call.TotalPackets++
			call.LastFrameNum = 0
		case uint(dmrconst.DTypeVoiceTerm):
			if call.LastFrameNum != dmrconst.VoiceF {
				call.LostSequences += dmrconst.VoiceF - call.LastFrameNum
				call.TotalPackets += dmrconst.VoiceF - call.LastFrameNum
			}
			call.TotalPackets++
			call.LastFrameNum = 0
		}
	case dmrconst.FrameVoiceSync:
		if !call.HasHeader && call.LastFrameNum == 0 {
			call.LostSequences++
			call.TotalPackets++
			call.HasHeader = true
		}
		if call.LastFrameNum != 0 && call.LastFrameNum != dmrconst.VoiceF {
			call.LostSequences += packet.DTypeOrVSeq - call.LastFrameNum - 1
			call.TotalPackets += packet.DTypeOrVSeq - call.LastFrameNum - 1
		}
		call.TotalPackets++
		call.LastFrameNum = packet.DTypeOrVSeq
	case dmrconst.FrameVoice:
		if !call.HasHeader {
			call.LostSequences += 1 + packet.DTypeOrVSeq
			call.TotalPackets += 1 + packet.DTypeOrVSeq
			call.HasHeader = true
		} else if packet.DTypeOrVSeq != call.LastFrameNum+1 {
			if packet.DTypeOrVSeq < call.LastFrameNum {
				call.LostSequences += dmrconst.VoiceF - call.LastFrameNum + packet.DTypeOrVSeq
				call.TotalPackets += dmrconst.VoiceF - call.LastFrameNum + packet.DTypeOrVSeq
			} else {
				call.LostSequences += packet.DTypeOrVSeq - call.LastFrameNum - 1
				call.TotalPackets += packet.DTypeOrVSeq - call.LastFrameNum - 1
			}
		}
		call.TotalPackets++
		call.LastFrameNum = packet.DTypeOrVSeq
	}
}
