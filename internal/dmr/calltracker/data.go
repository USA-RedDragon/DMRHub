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
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
)

const (
	subPreamble         = "preamble"
	subRadioCheck       = "radio_check"
	subRadioCheckAnswer = "radio_check_answer"
	subCallAlert        = "call_alert"
	subCallAlertAck     = "call_alert_ack"
	subResponse         = "response"
	subSMS              = "sms"
	subEncrypted        = "encrypted"
	subUnknownCSBK      = "csbk"
	subBadCRC           = "bad_crc"
	subUndecodable      = "undecodable"
	subReserved         = "reserved"
	subFrameType3       = "frame_type_3"
)

type class struct {
	kind      models.CallKind
	sub       string
	fid       *uint8
	opcode    *uint8
	header    bool
	block     bool
	cont      bool
	confirmed bool
	sap       *uint8
	resp      models.CallResponse
	answers   string
}

type singleKey struct {
	ch       chanKey
	src, dst uint
	group    bool
	kind     models.CallKind
	sub      string
}

func (s *shard) handle(t *task) {
	ch := s.channel(t.key, t.peer)
	ch.last = t.at
	s.heard(t)
	pc := t.p
	d := pc.Data()
	h := d.Hint(s.t.now())
	s.fixSeq(ch, &h)

	switch t.p.FrameType {
	case dmrconst.FrameVoice, dmrconst.FrameVoiceSync:
		s.voice(t, ch, &h, false)
	case dmrconst.FrameDataSync:
		switch enums.DataType(t.p.DTypeOrVSeq & 0xf) { //nolint:exhaustive
		case enums.DataTypeVoiceLCHeader, enums.DataTypeTerminatorWithLC:
			s.voice(t, ch, &h, false)
		case enums.DataTypePIHeader:
			s.voice(t, ch, &h, true)
		default:
			s.dataBurst(t, ch, &h)
		}
	default:
		s.single(t, ch, &class{kind: models.CallKindUnknown, sub: subFrameType3})
	}
}

func (s *shard) fixSeq(ch *channel, h *layer2.Hint) {
	raw := h.Seq & 0xff
	switch {
	case !ch.started:
		ch.started, ch.stream, ch.offset = true, uint(h.Stream), 0
	case uint(h.Stream) != ch.stream:
		ch.stream = uint(h.Stream)
		ch.offset = (ch.outSeq + 1 - raw) & 0xff
	}
	ch.outSeq = (raw + ch.offset) & 0xff
	h.Seq = ch.outSeq
}

func (s *shard) receiver(ch *channel) *data.Receiver {
	if ch.rx == nil {
		rx, err := data.NewReceiver(&data.ReceiverConfig{
			Respond:      false,
			AcceptBadCRC: true,
			Lost:         -1,
			Hang:         dataHang,
		})
		if err != nil {
			slog.Error("CallTracker: cannot make a data receiver", "error", err)
			return nil
		}
		ch.rx = rx
	}
	return ch.rx
}

func (s *shard) feed(t *task, ch *channel, h *layer2.Hint, cls *class) {
	rx := s.receiver(ch)
	if rx == nil {
		return
	}
	b := s.burst
	if cls == nil {
		opts := layer2.DecodeOptions{Kind: h.Kind, VoiceBurst: h.VoiceBurst}
		if h.HasDataType {
			dt := h.DataType
			opts.DataType = &dt
		}
		if err := b.DecodeFromBytesWith(layer2.RawBurst(t.p.DMRData), &opts); err != nil {
			b = nil
		}
	}
	s.events = rx.Decoded(s.t.now(), b, *h, s.events[:0])
	s.apply(ch, t, cls, s.events)
}

func (s *shard) dataBurst(t *task, ch *channel, h *layer2.Hint) {
	dt := enums.DataType(t.p.DTypeOrVSeq & 0xf)
	b := s.burst
	opts := layer2.DecodeOptions{Kind: layer2.KindData, DataType: &dt}
	if err := b.DecodeFromBytesWith(layer2.RawBurst(t.p.DMRData), &opts); err != nil || dt.IsReserved() || b.Data == nil {
		sub := subUndecodable
		if dt.IsReserved() {
			sub = subReserved
		}
		s.single(t, ch, &class{kind: models.CallKindUnknown, sub: sub})
		if ch.rx != nil {
			s.events = ch.rx.Decoded(s.t.now(), nil, *h, s.events[:0])
			s.apply(ch, t, nil, s.events)
		}
		return
	}
	cls := classify(b)
	if cls.block && ch.data != nil && !ch.data.finished {
		cls.kind = ch.data.call.Kind
	}
	s.feed(t, ch, h, &cls)
	s.place(t, ch, &cls)
}

func classify(b *layer2.Burst) class {
	switch d := b.Data.(type) {
	case *pdu.CSBK:
		return classifyCSBK(d)
	case *pdu.MBCHeader:
		if d.FEC.Uncorrectable {
			return class{kind: models.CallKindUnknown, sub: subBadCRC}
		}
		fid, op := uint8(d.FID), uint8(d.Opcode)
		sub := ""
		if d.FID == enums.FIDStandardized {
			sub = opcodeName(d.Opcode)
		}
		return class{kind: models.CallKindMBC, sub: sub, fid: &fid, opcode: &op}
	case *pdu.MBCContinuation:
		return class{kind: models.CallKindMBC, cont: true}
	case *pdu.DataHeader:
		return classifyHeader(d)
	case *pdu.Rate12Data, *pdu.Rate34Data, *pdu.Rate1Data:
		return class{kind: models.CallKindData, block: true}
	case *pdu.PRFill:
		return class{kind: models.CallKindIdle}
	case *pdu.UnifiedSingleBlockData:
		if d.FEC.Uncorrectable {
			return class{kind: models.CallKindUnknown, sub: subBadCRC}
		}
		return class{kind: models.CallKindUSBD, sub: strconv.Itoa(int(d.ServiceType))}
	}
	return class{kind: models.CallKindUnknown, sub: subUndecodable}
}

func classifyCSBK(d *pdu.CSBK) class {
	if d.FEC.Uncorrectable {
		return class{kind: models.CallKindUnknown, sub: subBadCRC}
	}
	fid, op := uint8(d.FID), uint8(d.Opcode)
	c := class{kind: models.CallKindCSBK, fid: &fid, opcode: &op}
	if _, ok := d.AsPreamble(); ok {
		c.sub = subPreamble
		return c
	}
	if rc, ok := d.AsMotorolaRadioCheck(); ok {
		if rc.Answer {
			c.sub, c.answers = subRadioCheckAnswer, subRadioCheck
		} else {
			c.sub = subRadioCheck
		}
		return c
	}
	if _, ok := d.AsMotorolaCallAlert(); ok {
		c.sub = subCallAlert
		return c
	}
	if _, ok := d.AsMotorolaCallAlertAck(); ok {
		c.sub, c.answers = subCallAlertAck, subCallAlert
		return c
	}
	name := opcodeName(d.Opcode)
	switch {
	case d.FID == enums.FIDStandardized && name != "":
		c.sub = name
	case d.FID == enums.FIDMotorola && (d.Opcode == pdu.CSBKCapacityPlusSiteStatus || d.Opcode == pdu.CSBKCapacityPlusNeighbors || d.Opcode == pdu.CSBKCapacityPlusDataRevertAnnouncement):
		c.sub = strings.TrimSuffix(d.Opcode.String(), " (FID 0x10)")
	default:
		c.kind, c.sub = models.CallKindUnknown, subUnknownCSBK
	}
	return c
}

func opcodeName(op pdu.CSBKOpcode) string {
	name := op.String()
	if strings.HasPrefix(name, "Unknown") || strings.Contains(name, "FID 0x10") {
		return ""
	}
	return strings.TrimSuffix(name, " PDU")
}

func classifyHeader(d *pdu.DataHeader) class {
	if d.FEC.Uncorrectable {
		return class{kind: models.CallKindUnknown, sub: subBadCRC}
	}
	c := class{kind: models.CallKindData, header: true}
	if sap, ok := d.SAP(); ok {
		v := uint8(sap)
		c.sap = &v
	}
	if _, ok := d.AsUDT(); ok {
		c.kind = models.CallKindUDT
		return c
	}
	if rp, ok := d.AsResponse(); ok {
		c.sub = subResponse
		if r, err := packet.ResponseOf(&rp); err == nil {
			c.resp = responseOf(r)
		}
		return c
	}
	if _, ok := d.AsConfirmed(); ok {
		c.confirmed = true
	} else if dd, ok := d.AsDefinedData(); ok {
		c.confirmed = dd.ResponseRequested
	} else if rd, ok := d.AsRawData(); ok {
		c.confirmed = rd.ResponseRequested
	}
	return c
}

func responseOf(r packet.Response) models.CallResponse {
	switch {
	case r == packet.ResponseACK:
		return models.CallResponseACK
	case r == packet.ResponseSACK:
		return models.CallResponseSACK
	case r.Class() == packet.ResponseNACKIllegalFormat.Class():
		return models.CallResponseNACK
	}
	return models.CallResponseNone
}

func (s *shard) single(t *task, ch *channel, cls *class) {
	p := &t.p
	if cls.answers != "" {
		want := cls.answers
		if s.t.answer(p.Src, p.Dst, models.CallResponseACK, func(kind models.CallKind, sub string) bool {
			return kind == models.CallKindCSBK && sub == want
		}) {
			return
		}
	}
	key := singleKey{ch: ch.key, src: p.Src, dst: p.Dst, group: p.GroupCall, kind: cls.kind, sub: cls.sub}
	r, ok := s.rows[key]
	if ok && t.at.Sub(r.last) > controlHang {
		s.finish(r, t.at)
		ok = false
	}
	if !ok {
		r = s.open(t, ch, key, cls.kind, cls.sub, controlHang)
		if r == nil {
			return
		}
		r.call.FID, r.call.Opcode = cls.fid, cls.opcode
	}
	s.touch(r, t)
	r.call.Bursts++
}

func (s *shard) place(t *task, ch *channel, cls *class) {
	switch {
	case cls.header || cls.block:
		r := s.dataRow(ch, t, cls)
		if r == nil {
			return
		}
		s.touch(r, t)
		r.call.Bursts++
		if !cls.header {
			return
		}
		if cls.sap != nil && r.call.SAP == nil {
			r.call.SAP = cls.sap
		}
		r.call.Confirmed = r.call.Confirmed || cls.confirmed
		if cls.sub == subResponse {
			r.call.SubKind = subResponse
			if cls.resp != models.CallResponseNone {
				r.call.Response = cls.resp
				s.t.answer(t.p.Src, t.p.Dst, cls.resp, answersData)
			}
		}
	case cls.cont:
		key := singleKey{ch: ch.key, src: t.p.Src, dst: t.p.Dst, group: t.p.GroupCall, kind: models.CallKindMBC}
		for k, r := range s.rows {
			sk, ok := k.(singleKey)
			if ok && sk.ch == key.ch && sk.src == key.src && sk.dst == key.dst && sk.kind == models.CallKindMBC {
				s.touch(r, t)
				r.call.Bursts++
				return
			}
		}
		s.single(t, ch, cls)
	default:
		s.single(t, ch, cls)
	}
}

func (s *shard) dataRow(ch *channel, t *task, cls *class) *row {
	r := ch.data
	if r != nil && r.finished {
		r = nil
	}
	if t == nil {
		return r
	}
	p := &t.p
	if r != nil && (r.call.SourceID != p.Src || r.call.DestinationID != p.Dst || r.call.GroupCall != p.GroupCall ||
		(cls != nil && cls.header && cls.kind != r.call.Kind)) {
		s.finish(r, t.at)
		r = nil
	}
	if r == nil {
		kind := models.CallKindData
		if cls != nil && (cls.kind == models.CallKindUDT || cls.kind == models.CallKindData) {
			kind = cls.kind
		}
		sub := ""
		if cls != nil && cls.sub == subResponse {
			sub = subResponse
		}
		r = s.open(t, ch, nil, kind, sub, dataRowHang)
		ch.data = r
	}
	return r
}

func (s *shard) apply(ch *channel, t *task, cls *class, events []data.Event) {
	for i := range events {
		ev := &events[i]
		switch ev.Kind {
		case data.EventDelivered, data.EventReassembled:
			r := s.dataRow(ch, t, cls)
			if r == nil {
				continue
			}
			s.delivered(r, ev)
			if o := s.t.dataObserver(); o != nil {
				o.DataPacket(r.call, ev, r.last)
			}
		case data.EventResponse:
			r := s.dataRow(ch, t, cls)
			if r == nil {
				continue
			}
			resp := responseOf(ev.Response)
			r.call.SubKind, r.call.Response = subResponse, resp
			if resp != models.CallResponseNone {
				s.t.answer(uint(ev.Call.Source), uint(ev.Call.Target), resp, answersData)
			}
			if o := s.t.dataObserver(); o != nil {
				o.DataResponse(r.call, ev, r.last)
			}
		case data.EventCallEnd:
			r := ch.data
			if r == nil || r.finished {
				continue
			}
			r.call.Blocks = uint(ev.Stats.Blocks)
			r.call.LostBlocks = uint(ev.Stats.Lost)
			r.call.BadCRC = uint(ev.Stats.BadCRC)
			s.finish(r, r.last)
		case data.EventCallStart, data.EventRespond:
		}
	}
}

func answersData(kind models.CallKind, sub string) bool {
	return (kind == models.CallKindData || kind == models.CallKindUDT) && sub != subResponse
}

func (s *shard) delivered(r *row, ev *data.Event) {
	m := &ev.Message
	if sap, ok := m.Header.SAP(); ok {
		v := uint8(sap)
		r.call.SAP = &v
	}
	switch {
	case ev.Kind == data.EventReassembled:
		r.call.Octets = uint(len(m.Payload))
	case m.Fragmented:
		r.call.Octets += uint(len(m.Payload))
		return
	default:
		r.call.Octets = uint(len(m.Payload))
	}
	if r.call.SubKind == subResponse {
		return
	}
	sub, format := classifyPayload(&m.Header, m.Payload)
	if sub != "" {
		r.call.SubKind, r.call.DataFormat = sub, format
	}
}

func classifyPayload(h *pdu.DataHeader, payload []byte) (sub, format string) {
	var buf [256]byte
	_, text, err := sms.Decode(buf[:0], h, payload)
	if err == nil || errors.Is(err, ip.ErrChecksum) {
		return subSMS, smsFormat(text.Format)
	}
	sap, ok := h.SAP()
	if !ok || (sap != enums.SAPIPBasedPacketData && sap != enums.SAPUDPIPHeaderCompression) {
		return "", ""
	}
	d, err := ip.Decode(sap, payload)
	if err != nil && !errors.Is(err, ip.ErrChecksum) {
		return "", ""
	}
	for _, port := range []ip.Port{d.DestinationPort, d.SourcePort} {
		if port == ip.PortLRRP {
			return "lrrp", ""
		}
		if port == ip.PortARS {
			return "ars", ""
		}
		if port == ip.PortTMS {
			return "tms", ""
		}
	}
	return "ip", ""
}

func smsFormat(f sms.Format) string {
	switch f {
	case sms.FormatTMS:
		return "tms"
	case sms.FormatIP:
		return "ip"
	case sms.FormatAnyTone:
		return "anytone"
	case sms.FormatDefinedData:
		return "defined_data"
	case sms.FormatHytera:
		return "hytera"
	}
	return "unknown"
}
