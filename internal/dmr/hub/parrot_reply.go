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
// call. The LC of the reply is that of the recording with the addresses of
// the reply, so its FID and Service Options pass. The last superframe of
// a call whose FID is not that of AMBE+2 is padded with the last recorded
// slot, as dmrgo pads a relayed call, because AMBE+2 silence is noise to
// another codec such as Codec2. The packets keep the call fields of the
// recording. When the recording holds no voice that the Receiver can
// place, parrotReply returns the recording as it is.
func parrotReply(packets []models.Packet, colorCode enums.ColorCode) []models.Packet {
	if len(packets) == 0 {
		return packets
	}
	slots, recorded, known := parrotVoiceSlots(packets)
	if len(slots) == 0 {
		return packets
	}
	total, last := len(slots), slots[len(slots)-1]

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
	call := parrotCall(&first, recorded, known)
	padLast := call.FID != enums.FIDStandardized && call.FID != enums.FIDMotorola
	if err := tx.Start(0, call, nil); err != nil {
		slog.Error("Parrot: cannot start reply", "error", err)
		return packets
	}

	reply := make([]models.Packet, 0, len(packets)+len(slots)/layer2.VoiceSlotsPerBurst+6)
	outputs := make([]voice.Output, 0, voice.MaxOutputs)
	stopped := false
	sent := 0
	for {
		for len(slots) > 0 && tx.Room() > 0 {
			if err := tx.VocoderSlot(slots[0]); err != nil {
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
				if padLast {
					pkt.DMRData = padWith(out.Burst, sent, total, last)
				}
				sent += layer2.VoiceSlotsPerBurst
			} else {
				pkt.FrameType = dmrconst.FrameDataSync
				pkt.DTypeOrVSeq = uint(out.Hint.DataType)
			}
			reply = append(reply, pkt)
		}
	}
	return reply
}

// parrotCall returns the call of the reply: from the source to the
// destination of the recorded packet first, with the LC of the recorded
// call rec, when known, readdressed.
func parrotCall(first *models.Packet, rec voice.Call, known bool) voice.Call {
	c := voice.Call{Group: first.GroupCall, Target: uint32(first.Dst & dmrconst.MaxDMRAddress), Source: uint32(first.Src & dmrconst.MaxDMRAddress)}
	if !known {
		return c
	}
	lc := rec.LC
	if err := lc.SetCall(c.Group, c.Target, c.Source); err != nil {
		return c
	}
	c.Options, c.FID, c.LC = rec.Options, rec.FID, lc
	return c
}

// padWith returns burst with each of its slots from the sent-th on that
// the recording does not have, the total-th and later, set to last.
func padWith(burst layer2.RawBurst, sent, total int, last [9]byte) layer2.RawBurst {
	if sent+layer2.VoiceSlotsPerBurst <= total {
		return burst
	}
	s := burst.VoiceSlots()
	for k := range s {
		if sent+k >= total {
			s[k] = last
		}
	}
	burst.SetVoiceSlots(s)
	return burst
}

// parrotVoiceSlots returns the vocoder slots of the voice bursts of the
// recorded packets, in the order a voice.Receiver places them, and the
// call that the Receiver learned from a Voice LC Header, an embedded LC or
// a Terminator with LC, with known false when it learned none.
func parrotVoiceSlots(packets []models.Packet) (slots [][9]byte, rec voice.Call, known bool) {
	rx, err := voice.NewReceiver(&voice.ReceiverConfig{Lost: -1, Hang: -1})
	if err != nil {
		slog.Error("Parrot: cannot make voice receiver", "error", err)
		return nil, rec, false
	}
	events := make([]voice.Event, 0, voice.MaxEvents)
	for i := range packets {
		pkt := packets[i]
		d := pkt.Data()
		raw := d.Burst
		now := time.Duration(i) * burstTime
		events = rx.Burst(now, &raw, d.Hint(now), events[:0])
		for j := range events {
			e := &events[j]
			switch {
			case e.Kind == voice.EventVoice:
				s := e.Burst.VoiceSlots()
				slots = append(slots, s[:]...)
			case !known && e.Call.Origin != voice.OriginUnknown && e.Call.Origin != voice.OriginExpected:
				rec, known = e.Call, true
			}
		}
	}
	return slots, rec, known
}
