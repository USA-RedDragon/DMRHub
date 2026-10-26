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

package modem

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/bs"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
)

const (
	// statusInterval is how often the host asks for the status, as
	// MMDVMHost does. The firmware goes idle after 2 s without it.
	statusInterval = 250 * time.Millisecond
	// silenceLimit is how long the modem may not answer a status request
	// before the link is down, as the inactivity timer of MMDVMHost.
	silenceLimit = 2 * time.Second
	// txLead is how far ahead of its time a burst goes to the modem, so
	// that its FIFO holds the next burst of each slot.
	txLead = 2 * bs.SlotTime
	// txQueue is the number of bursts per slot that wait for room in the
	// modem.
	txQueue = 32
	// maxTicks bounds the Repeater ticks of one tick of the radio.
	maxTicks = 16
	// csbkStreamHold is how long CSBKs between the same addresses share a
	// stream, as a Pre_CSBK train does.
	csbkStreamHold = time.Second
)

// sink is where the radio sends its frames and the packets for the hub.
type sink interface {
	write(frame []byte)
	route(packet models.Packet)
}

type slotState struct {
	call    voice.Call
	hasCall bool
	service bs.Service
	rf      bool
	stream  uint32
	seq     uint8

	csbkTarget, csbkSource uint32
	csbkStream             uint32
	csbkAt                 time.Duration

	queue [txQueue]layer2.RawBurst
	head  int
	count int
	space int
}

// radio is the host side of a link to a modem that is up: it feeds the
// frames of the modem and the packets of the hub to a bs.Repeater, sends
// its bursts to the modem and to the hub, keys the transmitter and sends
// the Short LC. It does no I/O of its own and is not safe for concurrent
// use.
type radio struct {
	m          *config.Modem
	repeater   uint32
	protocol   uint8
	rep        *bs.Repeater
	outs       []bs.Output
	out        sink
	keyed      bool
	slots      [2]slotState
	slc        mmdvm.DMRShortLC
	slcSent    bool
	lastStatus time.Duration
	nextStatus time.Duration
}

func newRadio(m *config.Modem, repeater uint32, protocol uint8, out sink, now time.Duration) (*radio, error) {
	rep, err := bs.New(&bs.Config{Mode: bs.ModeRF, ColorCode: colorCode(m), Lead: txLead})
	if err != nil {
		return nil, fmt.Errorf("make the repeater: %w", err)
	}
	return &radio{m: m, repeater: repeater, protocol: protocol, rep: rep, outs: make([]bs.Output, 0, bs.MaxOutputs),
		out: out, lastStatus: now, nextStatus: now}, nil
}

func slotIndex(s enums.Slot) (int, bool) {
	switch s {
	case enums.Slot1:
		return 0, true
	case enums.Slot2:
		return 1, true
	case enums.SlotUnknown:
	}
	return 0, false
}

func (r *radio) send(c mmdvm.Command, msg appender) {
	f, err := frame(c, msg)
	if err != nil {
		slog.Debug("Modem frame not sent", "command", c, "error", err)
		return
	}
	r.out.write(f)
}

// frame takes a frame from the modem.
func (r *radio) frame(now time.Duration, f mmdvm.Frame) {
	switch c := f.Command; {
	case c == mmdvm.CommandGetStatus:
		r.status(now, f.Payload)
	case c == mmdvm.CommandDMRData1, c == mmdvm.CommandDMRData2, c == mmdvm.CommandDMRLost1, c == mmdvm.CommandDMRLost2:
		r.dmr(now, f)
	case c == mmdvm.CommandNak:
		if n, err := mmdvm.DecodeNak(f.Payload); err == nil {
			slog.Warn("The modem refused a command", "command", n.Command, "reason", n.Reason)
		}
	case c == mmdvm.CommandDebugDump, c >= mmdvm.CommandDebug1 && c <= mmdvm.CommandDebug5:
		if d, err := mmdvm.DecodeDebug(f); err == nil && r.m.Debug {
			slog.Info("Modem debug", "text", string(d.Text), "numbers", d.N[:d.Count()])
		}
	case c == mmdvm.CommandAck, c == mmdvm.CommandGetVersion:
	default:
		slog.Debug("Ignoring a frame from the modem", "command", c, "length", len(f.Payload))
	}
	r.flush()
}

func (r *radio) status(now time.Duration, p []byte) {
	r.lastStatus = now
	var space1, space2 uint8
	var adc, dac, rxo, txo bool
	if r.protocol == 1 {
		s, err := mmdvm.DecodeStatusV1(p)
		if err != nil {
			slog.Debug("Bad status from the modem", "error", err)
			return
		}
		space1, space2, adc, dac, rxo, txo = s.DMRSpace1, s.DMRSpace2, s.ADCOverflow, s.DACOverflow, s.RXOverflow, s.TXOverflow
	} else {
		s, err := mmdvm.DecodeStatusV2(p)
		if err != nil {
			slog.Debug("Bad status from the modem", "error", err)
			return
		}
		space1, space2, adc, dac, rxo, txo = s.DMRSpace1, s.DMRSpace2, s.ADCOverflow, s.DACOverflow, s.RXOverflow, s.TXOverflow
	}
	r.slots[0].space, r.slots[1].space = int(space1), int(space2)
	for _, o := range []struct {
		set  bool
		what string
	}{{adc, "ADC levels"}, {dac, "DAC levels"}, {rxo, "RX buffer"}, {txo, "TX buffer"}} {
		if o.set {
			slog.Warn("The modem reports an overflow", "of", o.what)
		}
	}
}

func (r *radio) dmr(now time.Duration, f mmdvm.Frame) {
	d, err := mmdvm.DecodeDMRData(f)
	if err != nil {
		slog.Debug("Bad DMR frame from the modem", "command", f.Command, "error", err)
		return
	}
	if !r.m.Duplex && d.Slot != enums.Slot2 {
		return
	}
	rf := bs.Source{RF: true}
	if d.Lost {
		r.apply(now, r.rep.Lost(now, d.Slot, rf, r.outs[:0]))
		return
	}
	r.apply(now, r.rep.Inbound(now, &d.Burst, d.Hint(), rf, r.outs[:0]))
}

// packet takes a packet that the hub routed to the modem.
func (r *radio) packet(now time.Duration, p models.Packet) {
	if !r.m.Duplex && !p.Slot {
		return
	}
	d := p.Data()
	r.apply(now, r.rep.Inbound(now, &d.Burst, d.Hint(now), bs.Source{Peer: d.Stream}, r.outs[:0]))
	r.flush()
}

// tick runs the timers of the Repeater, asks for the status when it is
// due and sends the bursts that the modem has room for.
func (r *radio) tick(now time.Duration) {
	for range maxTicks {
		at, ok := r.rep.Deadline()
		if !ok || at > now {
			break
		}
		r.apply(now, r.rep.Tick(now, r.outs[:0]))
	}
	if now >= r.nextStatus {
		r.send(mmdvm.CommandGetStatus, nil)
		r.nextStatus = now + statusInterval
	}
	r.flush()
}

// deadline returns when the radio needs its next tick.
func (r *radio) deadline() time.Duration {
	next := min(r.nextStatus, r.lastStatus+silenceLimit)
	if at, ok := r.rep.Deadline(); ok {
		next = min(next, at)
	}
	return next
}

// silent reports whether the modem has not answered a status request for
// silenceLimit.
func (r *radio) silent(now time.Duration) bool {
	return now >= r.lastStatus+silenceLimit
}

// stop sends the frames that turn the transmitter off.
func (r *radio) stop() {
	if r.m.Duplex {
		r.send(mmdvm.CommandDMRStart, &mmdvm.DMRStart{TX: false})
	}
	r.send(mmdvm.CommandSetMode, &mmdvm.SetMode{Mode: mmdvm.ModeIdle})
}

func (r *radio) apply(now time.Duration, outs []bs.Output) {
	for i := range outs {
		o := &outs[i]
		switch o.Kind {
		case bs.OutputKey:
			r.key(true)
		case bs.OutputDekey:
			r.key(false)
		case bs.OutputBurst:
			r.burst(now, o)
		case bs.OutputCallStart:
			r.callStart(o)
		case bs.OutputCallEnd:
			slog.Info("Modem call ended", "slot", o.Slot, "rf", o.From.RF, "source", o.Call.Source, "target", o.Call.Target, "reason", o.End)
		case bs.OutputRejected:
			slog.Debug("Modem refused a source", "slot", o.Slot, "rf", o.From.RF, "reason", o.Reason, "target", o.Call.Target)
		}
	}
	r.shortLC()
}

func (r *radio) key(on bool) {
	r.keyed = on
	if on {
		r.send(mmdvm.CommandSetMode, &mmdvm.SetMode{Mode: mmdvm.ModeDMR})
		if r.m.Duplex {
			r.send(mmdvm.CommandDMRStart, &mmdvm.DMRStart{TX: true})
		}
		return
	}
	for i := range r.slots {
		r.slots[i].head, r.slots[i].count = 0, 0
	}
	r.stop()
}

func (r *radio) callStart(o *bs.Output) {
	i, ok := slotIndex(o.Slot)
	if !ok {
		return
	}
	s := &r.slots[i]
	s.call, s.hasCall, s.service, s.rf = o.Call, true, o.Service, o.From.RF
	if o.From.RF {
		s.stream, s.seq = newStream(), 0
	}
	slog.Info("Modem call", "slot", o.Slot, "rf", o.From.RF, "service", o.Service, "group", o.Call.Group,
		"source", o.Call.Source, "target", o.Call.Target)
}

func (r *radio) burst(now time.Duration, o *bs.Output) {
	i, ok := slotIndex(o.Slot)
	if !ok {
		return
	}
	if o.To&bs.ToNetwork != 0 && o.From.RF {
		r.toNetwork(now, &r.slots[i], o)
	}
	if o.To&bs.ToRF == 0 || !r.m.Duplex && (o.From.RF || o.Slot != enums.Slot2) {
		return
	}
	b := o.Burst
	if !r.m.Duplex {
		msSync(&b)
	}
	s := &r.slots[i]
	if s.count == len(s.queue) {
		slog.Debug("Modem TX queue full, dropping a burst", "slot", o.Slot)
		return
	}
	s.queue[(s.head+s.count)%len(s.queue)] = b
	s.count++
}

// msSync gives b the MS-sourced SYNC of its BS-sourced SYNC, which a
// simplex hotspot sends as MMDVMHost does in DMO.
func msSync(b *layer2.RawBurst) {
	p, dist := b.Sync()
	if dist != 0 {
		return
	}
	switch p { //nolint:exhaustive // only the BS-sourced patterns change
	case enums.SyncBSSourcedVoice:
		_ = b.SetSync(enums.SyncMSSourcedVoice)
	case enums.SyncBSSourcedData:
		_ = b.SetSync(enums.SyncMSSourcedData)
	}
}

func (r *radio) toNetwork(now time.Duration, s *slotState, o *bs.Output) {
	var group bool
	var target, source, stream uint32
	switch h := o.Hint; {
	case h.Kind == layer2.KindData && h.HasDataType && h.DataType == enums.DataTypeCSBK:
		p, res := o.Burst.BPTCPayload()
		c := pdu.CSBKBytes(p)
		var ok bool
		if target, source, ok = c.Addresses(); res.Uncorrectable || !ok {
			return
		}
		group, _ = c.Group()
		if s.csbkStream == 0 || s.csbkTarget != target || s.csbkSource != source || now > s.csbkAt+csbkStreamHold {
			s.csbkStream, s.csbkTarget, s.csbkSource = newStream(), target, source
		}
		s.csbkAt, stream = now, s.csbkStream
	case s.hasCall && s.rf:
		group, target, source, stream = s.call.Group, s.call.Target, s.call.Source, s.stream
	default:
		return
	}
	d := dmrd.Data{Seq: s.seq, Repeater: r.repeater, Stream: stream, Burst: o.Burst}
	if d.SetCall(group, target, source) != nil || d.SetHint(o.Hint) != nil {
		return
	}
	s.seq++
	r.out.route(models.PacketFromData(&d, false))
}

// flush sends the queued bursts that the modem has room for, as MMDVMHost
// does: while the last status gave more than one free burst.
func (r *radio) flush() {
	for i := range r.slots {
		s := &r.slots[i]
		slot := enums.Slot(i + 1) //nolint:gosec // 0 or 1
		for s.count > 0 && s.space > 1 {
			d := mmdvm.DMRData{Slot: slot, Burst: s.queue[s.head]}
			s.head, s.count = (s.head+1)%len(s.queue), s.count-1
			s.space--
			r.send(d.Command(), &d)
		}
	}
}

// shortLC sends the Short LC of the CACH when it changes, in duplex: an
// Activity Update for the slots with a call, or a Null message, as
// MMDVMHost DMRSlot::setShortLC.
func (r *radio) shortLC() {
	if !r.m.Duplex {
		return
	}
	var au pdu.ShortLCActivityUpdate
	var busy bool
	for i, s := range r.slots {
		act, ok := r.activity(i)
		if !ok {
			continue
		}
		hash, err := pdu.AddressHash(s.call.Target)
		if err != nil {
			continue
		}
		busy = true
		if i == 0 {
			au.TS1ActivityID, au.HashTS1 = act, hash
		} else {
			au.TS2ActivityID, au.HashTS2 = act, hash
		}
	}
	var slc pdu.ShortLC
	var err error
	if busy {
		err = slc.SetActivityUpdate(au)
	} else {
		err = slc.SetNullMessage(pdu.ShortLCNullMessage{})
	}
	if err != nil {
		return
	}
	enc, err := mmdvm.NewDMRShortLC(&slc)
	if err != nil || r.slcSent && enc == r.slc {
		return
	}
	r.slc, r.slcSent = enc, true
	r.send(mmdvm.CommandDMRShortLC, &enc)
}

func (r *radio) activity(i int) (enums.ActivityID, bool) {
	s := &r.slots[i]
	if !s.hasCall {
		return 0, false
	}
	switch r.rep.SlotState(enums.Slot(i + 1)) { //nolint:exhaustive,gosec // a slot has activity only while it carries a call
	case bs.SlotVoice:
		if s.service != bs.ServiceVoice {
			return 0, false
		}
		switch {
		case s.call.Group && s.call.Options.IsEmergency:
			return enums.ActivityEmergencyGroupVoice, true
		case s.call.Group:
			return enums.ActivityGroupVoice, true
		case s.call.Options.IsEmergency:
			return enums.ActivityEmergencyIndividualVoice, true
		}
		return enums.ActivityIndividualVoice, true
	case bs.SlotData:
		if s.call.Group {
			return enums.ActivityGroupData, true
		}
		return enums.ActivityIndividualData, true
	}
	return 0, false
}

// newStream returns a random stream ID other than 0.
func newStream() uint32 {
	var b [4]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return uint32(time.Now().UnixNano()) | 1 //nolint:gosec // only a fallback for a broken random source
		}
		if v := binary.BigEndian.Uint32(b[:]); v != 0 {
			return v
		}
	}
}
