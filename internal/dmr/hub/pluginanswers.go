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
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	l2packet "github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
)

const (
	// pluginAnswerSource is the server name of the source slots of the
	// answers of the hub to the texts to plugins.
	pluginAnswerSource = "pluginanswers"
	// pluginAnswerIdle is how long the hub keeps the data receiver of a
	// radio and a plugin's ID that gets no burst.
	pluginAnswerIdle = time.Minute
	// pluginAckWait bounds the send of a TMS acknowledgement.
	pluginAckWait = 30 * time.Second
	// slotTime is the time of one burst of a timeslot.
	slotTime = 60 * time.Millisecond
)

// PluginsOnline is the part of Plugins that tells whether a plugin has a
// stream to the hub.
type PluginsOnline interface {
	Online(plugin uint) bool
}

type answerKey struct {
	server   string
	repeater uint
	slot     bool
	src, dst uint
}

type answerLink struct {
	rx    *data.Receiver
	start time.Time
	last  time.Time
}

type pluginAnswers struct {
	mu     sync.Mutex
	links  map[answerKey]*answerLink
	events []data.Event
}

// answerPluginData feeds packet, a burst to an ID of plugin, to the data
// receiver of its radio and that ID, and answers a text of the radio as a
// radio would: with the response of the data link layer that a
// confirmed text asks for, and with the TMS acknowledgement that a TMS
// text asks for, in the rate of the text, through the repeater and
// timeslot that it came on. A packet that is not a text gets an ACK from
// no one, as the plugin may answer it itself. The hub answers only for a
// plugin that has a stream to it, and answers a SACK with a NACK, as some
// radios do not take a SACK.
func (h *Hub) answerPluginData(ctx context.Context, packet models.Packet, sourceName string, plugin uint) {
	if packet.GroupCall || packet.FrameType != dmrconst.FrameDataSync || packet.Repeater == 0 || h.getServerRole(sourceName) == RolePeer {
		return
	}
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin {
		return
	}
	if dt := enums.DataType(packet.DTypeOrVSeq & 0xF); dt == enums.DataTypeVoiceLCHeader || dt == enums.DataTypeTerminatorWithLC ||
		dt == enums.DataTypePIHeader || dt == enums.DataTypeIdle {
		return
	}
	key := answerKey{server: sourceName, repeater: packet.Repeater, slot: packet.Slot, src: packet.Src, dst: packet.Dst}
	s := &h.pluginAnswers
	s.mu.Lock()
	defer s.mu.Unlock()
	now := h.clock.Now()
	ln := s.link(key, now)
	if ln == nil {
		return
	}
	at := now.Sub(ln.start)
	raw := layer2.RawBurst(packet.DMRData)
	d := packet.Data()
	s.events = ln.rx.Burst(at, &raw, d.Hint(at), s.events[:0])
	text, rate, hasText := deliveredText(s.events)
	var responds bool
	for i := range s.events {
		if s.events[i].Kind == data.EventRespond {
			responds = true
		}
	}
	if (!responds && !hasText) || !h.pluginOnline(plugin) {
		return
	}
	last := at
	cc := raw.SlotType().ColorCode
	for i := range s.events {
		e := &s.events[i]
		if e.Kind != data.EventRespond || (e.Response == l2packet.ResponseACK && !hasText) {
			continue
		}
		burst, ok := e.Burst, true
		if e.Response == l2packet.ResponseSACK {
			burst, ok = nackOf(e.Burst, cc)
		}
		if !ok {
			continue
		}
		last = max(last, e.Due)
		h.answerAfter(key, burst, e.Due-at)
	}
	if hasText && text.Format == sms.FormatTMS && text.AckRequired && !text.Group {
		h.ackTMSAfter(ctx, key, text, rate, last-at+slotTime)
	}
}

func (s *pluginAnswers) link(key answerKey, now time.Time) *answerLink {
	if s.links == nil {
		s.links = map[answerKey]*answerLink{}
	}
	for k, ln := range s.links {
		if now.Sub(ln.last) > pluginAnswerIdle {
			delete(s.links, k)
		}
	}
	ln := s.links[key]
	if ln == nil {
		rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(key.dst), Respond: true, Source: layer2.SyncSourceBS, Lost: -1}) //nolint:gosec // a DMR ID is 24 bits
		if err != nil {
			slog.Error("Cannot make the data receiver that answers the texts to a plugin", "error", err)
			return nil
		}
		ln = &answerLink{rx: rx, start: now}
		s.links[key] = ln
	}
	ln.last = now
	return ln
}

func (h *Hub) pluginOnline(plugin uint) bool {
	p := h.plugins.Load()
	if p == nil {
		return false
	}
	if o, ok := (*p).(PluginsOnline); ok {
		return o.Online(plugin)
	}
	return true
}

// deliveredText returns the first text with a good CRC among events, its
// rate and true.
func deliveredText(events []data.Event) (sms.Text, enums.DataType, bool) {
	for i := range events {
		e := &events[i]
		if (e.Kind != data.EventDelivered && e.Kind != data.EventReassembled) || e.Message.BadCRC {
			continue
		}
		if t, ok := plugin.IsText(&e.Message); ok {
			return t, e.Message.Rate, true
		}
	}
	return sms.Text{}, 0, false
}

// nackOf returns the first burst of a SACK, its header, as a NACK for a
// packet CRC error with the same addresses and N(S), and false for the
// data blocks of the SACK, which the NACK has none of.
func nackOf(burst layer2.RawBurst, cc enums.ColorCode) (layer2.RawBurst, bool) {
	if burst.SlotType().DataType != enums.DataTypeDataHeader {
		return burst, false
	}
	b := layer2.NewBurst()
	dt := enums.DataTypeDataHeader
	if err := b.DecodeFromBytesWith(burst, &layer2.DecodeOptions{Kind: layer2.KindData, DataType: &dt}); err != nil {
		return burst, false
	}
	dh, ok := b.Data.(*pdu.DataHeader)
	if !ok {
		return burst, false
	}
	r, ok := dh.AsResponse()
	if !ok {
		return burst, false
	}
	r.BlocksToFollow, r.ResponseClass, r.ResponseType = 0, l2packet.ResponseNACKPacketCRC.Class(), l2packet.ResponseNACKPacketCRC.Type()
	var out pdu.DataHeader
	if err := out.SetResponse(r); err != nil {
		return burst, false
	}
	enc, err := out.Encode()
	if err != nil {
		return burst, false
	}
	nack, err := layer2.BuildDataBurst(enc, enums.DataTypeDataHeader, cc, layer2.SyncSourceBS)
	if err != nil {
		return burst, false
	}
	return nack, true
}

// answerAfter sends burst, from the plugin's ID to the radio of key,
// through the repeater and timeslot of key, after wait.
func (h *Hub) answerAfter(key answerKey, burst layer2.RawBurst, wait time.Duration) {
	p := models.Packet{
		Signature: string(dmrconst.CommandDMRD), Src: key.dst, Dst: key.src, Slot: key.slot, Repeater: key.repeater,
		FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(burst.SlotType().DataType), StreamID: newStreamID(),
		DMRData: burst, BER: -1, RSSI: -1,
	}
	send := func() {
		h.publishToRepeater(sourceSlot{streamSource: streamSource{server: pluginAnswerSource, repeater: key.repeater}, slot: key.slot}, key.repeater, p)
	}
	if wait <= 0 {
		send()
		return
	}
	h.clock.AfterFunc(wait, send)
}

// ackTMSAfter sends the TMS acknowledgement of text, at rate, from the
// plugin's ID to the radio of key after wait. It goes on after ctx, the
// context of the routing of the text, ends.
func (h *Hub) ackTMSAfter(ctx context.Context, key answerKey, text sms.Text, rate enums.DataType, wait time.Duration) {
	var hd sms.Header
	payload, err := sms.AppendAck(nil, &text, &hd)
	if err != nil {
		slog.Error("Cannot build the TMS acknowledgement of a text to a plugin", "src", key.src, "dst", key.dst, "error", err)
		return
	}
	m := data.Message{Header: hd.Data, Rate: rate, Payload: payload}
	h.clock.AfterFunc(max(wait, 0), func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pluginAckWait)
		defer cancel()
		if _, err := h.SendData(ctx, key.repeater, key.slot, m); err != nil {
			slog.Warn("Cannot send the TMS acknowledgement of a text to a plugin", "src", key.src, "dst", key.dst, "error", err)
		}
	})
}
