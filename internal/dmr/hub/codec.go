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
	"strconv"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
)

const (
	// codecChangedTopic carries "origin:repeaterID" when the codec of a
	// timeslot of the repeater changes, so the other replicas drop their
	// copy of the repeater and the list of Codec2 timeslots.
	codecChangedTopic = "hub:repeaters:codec"
	heardQueue        = 256
)

// streamCodec is what the hub knows about the codec of one stream that it
// routes. Only the routing worker of the stream uses it, apart from last
// and tx, which mu guards.
type streamCodec struct {
	codec     transcode.Codec
	header    bool
	embedded  bool
	lcs       transcode.EmbeddedCodec
	encrypted bool
	learns    bool
	repeater  uint
	slot      bool
	det       transcode.ModeDetector
	mode      codec2.Mode
	noted     bool
	ended     bool
	need      bool
	needAt    time.Time
	tx        *transcoder

	mu   sync.Mutex
	last time.Time
}

func (sc *streamCodec) touch(now time.Time) {
	sc.mu.Lock()
	sc.last = now
	sc.mu.Unlock()
}

func (sc *streamCodec) idle(now time.Time) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return now.Sub(sc.last) > streamIdle
}

type heardNote struct {
	repeater uint
	slot     bool
	codec    transcode.Codec
	mode     codec2.Mode
	at       time.Time
}

// observeCodec sets packet.Codec to the codec of its stream, which the
// Voice LC Header of the stream decides: Codec2 for the FID 0xC2, AMBE
// for any other. A stream without a valid header takes the codec from the
// first embedded voice LC that passes its checksum, in the same way; its
// bursts before that LC take the codec of the timeslot they come from, and
// AMBE from a network peer. A header later in the stream still decides.
// The LC that decides the codec of a call from a repeater timeslot records
// the codec on the timeslot, and the Codec2 mode that the frames of a
// Codec2 call show. A call without such an LC teaches nothing. It returns
// nil for a burst of a stream that has had no voice.
func (h *Hub) observeCodec(packet *models.Packet, sourceName string) *streamCodec {
	now := time.Now()
	voice := transcode.IsVoice(packet) || isVoiceLC(packet)
	sc, loaded := h.codecStreams.Load(packet.StreamID)
	if loaded && sc.ended && transcode.IsHeader(packet) {
		loaded = false
	}
	if !loaded {
		if !voice {
			return nil
		}
		sc = &streamCodec{last: now}
		_, isPlugin := plugins.PluginOfSource(sourceName)
		sc.learns = !isPlugin && h.getServerRole(sourceName) == RoleRepeater && sourceName != models.RepeaterTypeIPSC
		sc.repeater, sc.slot = packet.Repeater, packet.Slot
		h.codecStreams.Store(packet.StreamID, sc)
	}
	sc.touch(now)
	if sc.ended {
		if voice {
			packet.Codec = uint8(sc.codec)
		}
		return sc
	}
	if codec, lc, ok := transcode.HeaderCodec(packet); ok && !sc.header {
		sc.header = true
		h.decideCodec(sc, codec, lc, now)
	} else if !sc.header && !sc.embedded {
		if codec, lc, ok := sc.lcs.Add(packet); ok {
			sc.embedded = true
			h.decideCodec(sc, codec, lc, now)
		}
	}
	if dt, ok := transcode.DataTypeOf(packet); ok && dt == enums.DataTypePIHeader {
		sc.encrypted = true
	}
	if !voice {
		return sc
	}
	if sc.codec == transcode.CodecUnknown {
		sc.codec = h.slotCodec(sc)
	}
	packet.Codec = uint8(sc.codec)
	if (sc.header || sc.embedded) && sc.codec == transcode.CodecCodec2 && transcode.IsVoice(packet) && sc.mode == codec2.ModeAuto {
		for _, s := range layer2.RawBurst(packet.DMRData).VoiceSlots() {
			sc.det.Add(s)
		}
		if m := sc.det.Mode(); m != codec2.ModeAuto {
			sc.mode = m
			h.noteCallCodec(packet, sc)
			if sc.learns {
				h.noteHeard(heardNote{repeater: sc.repeater, slot: sc.slot, codec: sc.codec, mode: m, at: now})
			}
		}
	}
	if !sc.noted {
		sc.noted = true
		h.noteCallCodec(packet, sc)
	}
	return sc
}

// decideCodec makes codec, from the voice LC lc of a header or of the
// embedded LC, the codec of the stream and of its call row, and records it
// on the timeslot of a stream that teaches it.
func (h *Hub) decideCodec(sc *streamCodec, codec transcode.Codec, lc pdu.LCBytes, now time.Time) {
	if sc.codec != codec {
		sc.setTranscoder(nil)
		sc.needAt = time.Time{}
		sc.mode, sc.det = codec2.ModeAuto, transcode.ModeDetector{}
		sc.noted = false
	}
	sc.codec = codec
	sc.encrypted = transcode.Encrypted(lc)
	if sc.learns {
		h.noteHeard(heardNote{repeater: sc.repeater, slot: sc.slot, codec: codec, at: now})
	}
}

func (h *Hub) noteCallCodec(packet *models.Packet, sc *streamCodec) {
	if h.callTracker != nil {
		h.callTracker.VoiceCodec(*packet, sc.codec.String(), int(sc.mode))
	}
}

func isVoiceLC(p *models.Packet) bool {
	dt, ok := transcode.DataTypeOf(p)
	return ok && (dt == enums.DataTypeVoiceLCHeader || dt == enums.DataTypeTerminatorWithLC)
}

// slotCodec returns the codec of a stream without a Voice LC Header: that
// of its timeslot, or AMBE.
func (h *Hub) slotCodec(sc *streamCodec) transcode.Codec {
	if !sc.learns {
		return transcode.CodecAMBE
	}
	rpt, err := h.routingRepeater(sc.repeater)
	if err != nil {
		return transcode.CodecAMBE
	}
	return transcode.ParseCodec(rpt.SlotCodec(sc.slot))
}

func (h *Hub) noteHeard(n heardNote) {
	select {
	case h.heard <- n:
	default:
		slog.Warn("Dropping the codec heard on a timeslot, the queue is full", "repeaterID", n.repeater)
	}
}

func (h *Hub) runHeard() {
	for {
		select {
		case <-h.done:
			return
		case n := <-h.heard:
			h.recordHeard(n)
		}
	}
}

func (h *Hub) recordHeard(n heardNote) {
	before, err := h.waitRepeater(n.repeater)
	if err != nil {
		return
	}
	heard, mode := before.TS1HeardCodec, before.TS1HeardCodec2Mode
	if n.slot {
		heard, mode = before.TS2HeardCodec, before.TS2HeardCodec2Mode
	}
	if err := models.SetRepeaterHeardCodec(h.db, n.repeater, n.slot, n.codec.String(), int(n.mode), n.at); err != nil {
		slog.Error("Cannot record the codec heard on a timeslot", "repeaterID", n.repeater, "error", err)
		return
	}
	if heard == n.codec.String() && (n.mode == codec2.ModeAuto || mode == int(n.mode)) {
		return
	}
	slog.Info("Timeslot codec heard", "repeaterID", n.repeater, "slot2", n.slot, "codec", n.codec.String(), "mode", int(n.mode))
	h.CodecChanged(n.repeater)
}

// CodecChanged drops what every replica knows of the codecs of the
// repeater id. Call it after a change of the codec of one of its
// timeslots.
func (h *Hub) CodecChanged(id uint) {
	h.reloadRepeater(id)
	h.loadCodec2Repeaters()
	if h.pubsub == nil {
		return
	}
	msg := h.origin + ":" + strconv.FormatUint(uint64(id), 10)
	if err := h.pubsub.Publish(codecChangedTopic, []byte(msg)); err != nil {
		slog.Error("Failed to announce a codec change", "repeaterID", id, "error", err)
	}
}

func (h *Hub) listenCodecChanges(sub pubsub.Subscription) {
	defer func() {
		if err := sub.Close(); err != nil {
			slog.Error("Error closing codec change subscription", "error", err)
		}
	}()
	ch := sub.Channel()
	ticker := time.NewTicker(repeaterCacheTTL)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.loadCodec2Repeaters()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			origin, id, valid := parseRepeaterChange(msg)
			if !valid || origin == h.origin {
				continue
			}
			h.reloadRepeater(id)
			h.loadCodec2Repeaters()
		}
	}
}

func (h *Hub) pruneCodecStreams(now time.Time) {
	h.codecStreams.Range(func(id uint, sc *streamCodec) bool {
		if sc.idle(now) {
			h.codecStreams.Delete(id)
			sc.setTranscoder(nil)
		}
		return true
	})
	pruneStreams(h.copyReports, now)
	h.tgRepeaters.Prune(now)
}

// endCodecStream stops the transcoder of a stream at its terminator. The
// stream stays known as ended until it idles, so that a late or repeated
// burst after the terminator keeps the codec of the call; a new Voice LC
// Header with the same stream ID starts a new call.
func (h *Hub) endCodecStream(stream uint) {
	if sc, ok := h.codecStreams.Load(stream); ok {
		sc.ended = true
		sc.setTranscoder(nil)
	}
}
