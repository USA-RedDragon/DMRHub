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
	"encoding/json"
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// convertedTopic carries a codecNote of a target that got a call
// converted, from the replica that delivered it to the one that tracks the
// call.
const convertedTopic = "calltracker:converted"

type codecNote struct {
	Stream   uint   `json:"stream"`
	Track    uint64 `json:"track"`
	Codec    string `json:"codec,omitempty"`
	Mode     int    `json:"mode,omitempty"`
	MissedMS int    `json:"missed_ms,omitempty"`
	Busy     bool   `json:"busy,omitempty"`
	To       string `json:"to,omitempty"`
	Target   string `json:"target,omitempty"`
}

// VoiceCodec records on the voice call of p, a packet that Track got here,
// its codec, "ambe" or "codec2", and a Codec2 mode other than 0.
func (c *CallTracker) VoiceCodec(p models.Packet, codec string, mode int) {
	c.noteCodec(codecNote{Stream: p.StreamID, Track: p.TrackID, Codec: codec, Mode: mode})
}

// Missed adds ms milliseconds of audio that could not be converted to the
// voice call of p, a packet that Track got here.
func (c *CallTracker) Missed(p models.Packet, ms int) {
	c.noteCodec(codecNote{Stream: p.StreamID, Track: p.TrackID, MissedMS: ms})
}

// Busy marks the voice call of p, a packet that Track got here, as not
// converted because every AMBE server was busy.
func (c *CallTracker) Busy(p models.Packet) {
	c.noteCodec(codecNote{Stream: p.StreamID, Track: p.TrackID, Busy: true})
}

// Converted records that target, such as "repeater:311860", got the voice
// call of p converted to codec. Any replica may call it: a call that
// another replica tracks gets it over pubsub.
func (c *CallTracker) Converted(p models.Packet, codec, target string) {
	n := codecNote{Stream: p.StreamID, Track: p.TrackID, To: codec, Target: target}
	if c.noteCodec(n) || c.pubsub == nil {
		return
	}
	msg, err := json.Marshal(n)
	if err != nil {
		return
	}
	c.publishLater(convertedTopic, msg)
}

func (c *CallTracker) remoteConverted(msg []byte) {
	var n codecNote
	if err := json.Unmarshal(msg, &n); err != nil || n.Target == "" {
		return
	}
	c.noteCodec(n)
}

// noteCodec queues n on the worker of its call and reports whether the
// call is tracked here.
func (c *CallTracker) noteCodec(n codecNote) bool {
	if c.stopped.Load() {
		return false
	}
	ref, ok := c.streams.Load(n.Stream)
	if !ok {
		return false
	}
	idx := ref.shard
	if n.Track != 0 {
		idx = int(n.Track % shardCount)
	}
	c.enqueue(idx, task{fn: func(s *shard) { s.codecNote(n) }})
	return true
}

func (s *shard) codecNote(n codecNote) {
	st, ok := s.streams[n.Stream]
	if !ok {
		return
	}
	r := st.row(n.Track)
	if r == nil || r.deleted || r.call.Kind != models.CallKindVoice {
		return
	}
	call := r.call
	changed := false
	if n.Codec != "" && call.Codec != n.Codec {
		call.Codec, changed = n.Codec, true
	}
	if n.Mode != 0 && call.Codec2Mode != n.Mode {
		call.Codec2Mode, changed = n.Mode, true
	}
	if n.MissedMS > 0 {
		call.MissedMS += n.MissedMS
		changed = true
	}
	if n.Busy && !call.TranscodeBusy {
		call.TranscodeBusy, changed = true, true
	}
	if n.Target != "" {
		if r.converted == nil {
			r.converted = map[string]bool{}
		}
		key := n.To + "/" + n.Target
		if !r.converted[key] {
			r.converted[key] = true
			switch n.To {
			case models.CodecAMBE:
				call.SentAMBE++
			case models.CodecCodec2:
				call.SentCodec2++
			}
			changed = true
		}
	}
	if !changed || !r.finished {
		return
	}
	if err := s.t.db.Model(&models.Call{}).Where("id = ?", call.ID).
		Select("codec", "codec2_mode", "missed_ms", "sent_ambe", "sent_codec2", "transcode_busy").
		Updates(&models.Call{Codec: call.Codec, Codec2Mode: call.Codec2Mode, MissedMS: call.MissedMS, SentAMBE: call.SentAMBE, SentCodec2: call.SentCodec2, TranscodeBusy: call.TranscodeBusy}).Error; err != nil {
		slog.Error("CallTracker: error saving the codec of a call", "error", err)
		return
	}
	s.t.publishCall(s.idx, call)
}
