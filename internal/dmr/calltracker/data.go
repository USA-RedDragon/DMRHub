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
	answerData          = "data"
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
	ns        uint8
	hasNS     bool
	fragment  bool
}

type dataKey struct {
	src, dst uint
	group    bool
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
	if !ch.peer && ch.hasStreamID && ch.streamID != t.p.StreamID {
		s.newStream(ch, t)
	}
	ch.streamID, ch.hasStreamID = t.p.StreamID, true
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

// newStream ends the data and control rows of the previous stream on the
// repeater slot ch, where a new stream starts with t. A repeater slot
// carries one transmission at a time, so a new stream right after another
// is a new transmission, not more of the last one. Voice rows end on their
// own key and hang.
func (s *shard) newStream(ch *channel, t *task) {
	if ch.rx != nil {
		s.events = ch.rx.Lost(s.t.now(), s.events[:0])
		s.apply(ch, nil, nil, s.events)
		ch.rx = nil
	}
	for _, r := range ch.data {
		s.finish(r, t.at)
	}
	for k, r := range s.rows {
		if sk, ok := k.(singleKey); ok && sk.ch == ch.key && r.call.StreamID != t.p.StreamID {
			s.finish(r, t.at)
		}
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
	if r := ch.data[dataKey{src: t.p.Src, dst: t.p.Dst, group: t.p.GroupCall}]; cls.block && r != nil && !r.finished {
		cls.kind = r.call.Kind
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
		return class{kind: models.CallKindUSBD, sub: SubKindName(d.ServiceType.String())}
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
		c.sub = SubKindName(d.Opcode.String())
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
	return SubKindName(name)
}

// SubKindName returns the sub kind for the dmrgo name of a CSBK opcode, a
// SAP, a port, a USBD service type or a text format: the name in lower
// case, without " PDU" and " (FID 0x10)", with each run of other
// characters than letters and digits as one "_". For example, the name
// "BS Outbound Activation PDU or TD_GRANT_MI PDU" gives
// "bs_outbound_activation_or_td_grant_mi".
func SubKindName(name string) string {
	name = strings.ReplaceAll(name, " PDU", "")
	name = strings.ReplaceAll(name, " (FID 0x10)", "")
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if sep && b.Len() > 0 {
				b.WriteByte('_')
			}
			sep = false
			b.WriteRune(r)
			continue
		}
		sep = true
	}
	return b.String()
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
	if ch, ok := d.AsConfirmed(); ok {
		c.confirmed, c.ns, c.hasNS = true, ch.SendSequenceNumber, true
		c.fragment = ch.FragmentSequenceNumber&0x7 != 0
	} else if u, ok := d.AsUnconfirmed(); ok {
		c.fragment = u.FragmentSequenceNumber&0x7 != 0
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
		if s.t.answer(p.Src, p.Dst, models.CallResponseACK, cls.answers) {
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
		s.t.remember(s, r)
		if cls.answers != "" {
			k := pair{src: p.Dst, dst: p.Src}
			s.t.keepAnswer(k, cls.answers, pendingAnswer{RowID: r.call.ID, Response: models.CallResponseACK})
			if s.t.answer(p.Src, p.Dst, models.CallResponseACK, cls.answers) {
				s.t.takeAnswer(k, cls.answers)
				s.drop(r)
				return
			}
		}
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
		if cls.block {
			r.call.Blocks++
			return
		}
		if cls.sap != nil && r.call.SAP == nil {
			r.call.SAP = cls.sap
		}
		r.call.Confirmed = r.call.Confirmed || cls.confirmed
		r.ns, r.hasNS = cls.ns, cls.hasNS
		if cls.sub == subResponse {
			r.call.SubKind = subResponse
			r.delivered = true
			if cls.resp != models.CallResponseNone {
				r.call.Response = cls.resp
				s.t.answer(t.p.Src, t.p.Dst, cls.resp, answerData)
			}
		}
		if !r.remembered {
			r.remembered = true
			s.t.remember(s, r)
		}
	case cls.cont:
		for k, r := range s.rows {
			sk, ok := k.(singleKey)
			if ok && sk.ch == ch.key && sk.src == t.p.Src && sk.dst == t.p.Dst && sk.kind == models.CallKindMBC {
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

func newPacket(r *row, cls *class) bool {
	switch {
	case cls == nil || !cls.header:
		return false
	case cls.kind != r.call.Kind:
		return true
	case (cls.sub == subResponse) != (r.call.SubKind == subResponse):
		return true
	case !r.delivered:
		return false
	case cls.fragment:
		return false
	case cls.hasNS && r.hasNS && cls.ns == r.ns:
		return false
	}
	return true
}

func (s *shard) dataRow(ch *channel, t *task, cls *class) *row {
	if t == nil {
		return nil
	}
	p := &t.p
	key := dataKey{src: p.Src, dst: p.Dst, group: p.GroupCall}
	r := ch.data[key]
	if r != nil && (r.finished || newPacket(r, cls)) {
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
		r = s.adoptPreambles(ch, t, kind, sub)
		if r == nil {
			r = s.open(t, ch, nil, kind, sub, dataRowHang)
		}
		if r == nil {
			return nil
		}
		r.dataKey = &key
		ch.data[key] = r
	}
	return r
}

// adoptPreambles returns the open row of the preamble CSBKs that announced
// the data packet of t on its stream, made into the row of the packet, so
// that a preamble and its data make one transmission. It returns nil when
// there is none.
func (s *shard) adoptPreambles(ch *channel, t *task, kind models.CallKind, sub string) *row {
	p := &t.p
	key := singleKey{ch: ch.key, src: p.Src, dst: p.Dst, group: p.GroupCall, kind: models.CallKindCSBK, sub: subPreamble}
	r, ok := s.rows[key]
	if !ok || r.finished || r.call.StreamID != p.StreamID || t.at.Sub(r.last) > controlHang {
		return nil
	}
	delete(s.rows, key)
	r.key = nil
	r.hang = dataRowHang
	r.call.Kind, r.call.SubKind = kind, sub
	r.call.FID, r.call.Opcode = nil, nil
	return r
}

func (s *shard) messageRow(ch *channel, t *task, cls *class, m *data.Message) *row {
	if t != nil {
		return s.dataRow(ch, t, cls)
	}
	dst, src, ok := m.Header.Addresses()
	if !ok {
		return nil
	}
	for _, group := range []bool{false, true} {
		if r := ch.data[dataKey{src: uint(src), dst: uint(dst), group: group}]; r != nil && !r.finished {
			return r
		}
	}
	return nil
}

func (s *shard) apply(ch *channel, t *task, cls *class, events []data.Event) {
	for i := range events {
		ev := &events[i]
		switch ev.Kind {
		case data.EventDelivered, data.EventReassembled:
			r := s.messageRow(ch, t, cls, &ev.Message)
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
			if o := s.t.dataObserver(); o != nil {
				o.DataResponse(r.call, ev, r.last)
			}
		case data.EventCallEnd:
			if r := ch.data[dataKey{src: uint(ev.Call.Source), dst: uint(ev.Call.Target), group: ev.Call.Group}]; r != nil && !r.finished {
				r.call.LostBlocks = uint(ev.Stats.Lost)
			}
			for _, r := range ch.data {
				s.finish(r, r.last)
			}
		case data.EventCallStart, data.EventRespond:
		}
	}
}

func (s *shard) delivered(r *row, ev *data.Event) {
	m := &ev.Message
	if ev.Kind == data.EventDelivered {
		r.delivered = true
		if m.BadCRC {
			r.call.BadCRC++
		}
	}
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
	if !ok {
		return "", ""
	}
	if sap != enums.SAPIPBasedPacketData && sap != enums.SAPUDPIPHeaderCompression {
		return SubKindName(sap.String()), ""
	}
	d, err := ip.Decode(sap, payload)
	if err != nil && !errors.Is(err, ip.ErrChecksum) {
		return "", ""
	}
	for _, port := range []ip.Port{d.DestinationPort, d.SourcePort} {
		if name := port.String(); name != strconv.Itoa(int(port)) {
			return SubKindName(name), ""
		}
	}
	return "ip", ""
}

func smsFormat(f sms.Format) string {
	switch f {
	case sms.FormatTMS, sms.FormatIP, sms.FormatAnyTone, sms.FormatDefinedData, sms.FormatHytera:
		return SubKindName(f.String())
	}
	return "unknown"
}
