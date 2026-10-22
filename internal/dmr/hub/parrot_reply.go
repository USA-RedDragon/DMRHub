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

package hub

import (
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
)

// burstTime is the time of one DMR burst, 60 ms.
const burstTime = 60 * time.Millisecond

// parrotReply returns the packets that play back the recorded parrot call
// packets as a new call from the parrot to the caller. A dmrgo
// voice.Receiver takes the voice out of the recording and a
// voice.Transmitter sends it again, so that the Voice LC Header, the
// embedded LC and the Terminator with LC name the reply, not the recorded
// call. The packets keep the call fields of the recording. When the
// recording holds no voice that the Receiver can place, parrotReply returns
// the recording as it is.
func parrotReply(packets []models.Packet, colorCode enums.ColorCode) []models.Packet {
	if len(packets) == 0 {
		return packets
	}
	slots := parrotVoiceSlots(packets)
	if len(slots) == 0 {
		return packets
	}

	tx, err := voice.NewTransmitter(&voice.TransmitterConfig{
		ColorCode: colorCode,
		Source:    layer2.SyncSourceBS,
		Silence:   vocoder.AMBESilenceSlot(),
		Timeout:   -1,
	})
	if err != nil {
		slog.Error("Parrot: cannot make voice transmitter", "error", err)
		return packets
	}
	first := packets[0]
	call := voice.Call{Group: first.GroupCall, Target: uint32(first.Dst & dmrconst.MaxDMRAddress), Source: uint32(first.Src & dmrconst.MaxDMRAddress)}
	if err := tx.Start(0, call, nil); err != nil {
		slog.Error("Parrot: cannot start reply", "error", err)
		return packets
	}

	reply := make([]models.Packet, 0, len(packets)+len(slots)/layer2.VoiceSlotsPerBurst+6)
	outputs := make([]voice.Output, 0, voice.MaxOutputs)
	stopped := false
	for {
		for len(slots) > 0 && tx.Room() > 0 {
			if err := tx.Slot(slots[0]); err != nil {
				slog.Error("Parrot: cannot queue voice", "error", err)
				return packets
			}
			slots = slots[1:]
		}
		if len(slots) == 0 && !stopped {
			tx.Stop()
			stopped = true
		}
		at, ok := tx.Deadline()
		if !ok {
			break
		}
		outputs = tx.Tick(at, outputs[:0])
		for i := range outputs {
			out := &outputs[i]
			if out.Kind == voice.OutputEnded {
				return reply
			}
			pkt := first
			pkt.Seq = uint(len(reply) & 0xFF)
			pkt.DMRData = out.Burst
			if idx, ok := out.Hint.VoiceBurst.Index(); out.Hint.Kind == layer2.KindVoice && ok {
				pkt.FrameType = dmrconst.FrameVoice
				if idx == 0 {
					pkt.FrameType = dmrconst.FrameVoiceSync
				}
				pkt.DTypeOrVSeq = uint(idx) //nolint:gosec // idx is 0 to 5
			} else {
				pkt.FrameType = dmrconst.FrameDataSync
				pkt.DTypeOrVSeq = uint(out.Hint.DataType)
			}
			reply = append(reply, pkt)
		}
	}
	return reply
}

// parrotVoiceSlots returns the vocoder slots of the voice bursts of the
// recorded packets, in the order a voice.Receiver places them.
func parrotVoiceSlots(packets []models.Packet) [][9]byte {
	rx, err := voice.NewReceiver(&voice.ReceiverConfig{Lost: -1, Hang: -1})
	if err != nil {
		slog.Error("Parrot: cannot make voice receiver", "error", err)
		return nil
	}
	var slots [][9]byte
	events := make([]voice.Event, 0, voice.MaxEvents)
	for i := range packets {
		pkt := packets[i]
		d := pkt.Data()
		raw := d.Burst
		events = rx.Burst(time.Duration(i)*burstTime, &raw, d.Hint(), events[:0])
		for j := range events {
			if events[j].Kind == voice.EventVoice {
				s := events[j].Burst.VoiceSlots()
				slots = append(slots, s[:]...)
			}
		}
	}
	return slots
}
