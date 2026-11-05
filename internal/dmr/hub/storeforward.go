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
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/puzpuzpuz/xsync/v4"
)

const (
	storeForwardSource       = "storeforward"
	storeForwardResponseWait = 3 * time.Second
	storeForwardRetries      = 3
	tapDepth                 = 64
)

var errNoAddresses = errors.New("the data header has no addresses")

// StoreForward is told of the private data that the hub cannot route and
// of the radios that it hears, for store and forward.
type StoreForward interface {
	// NotRouted is called for each burst of a private data stream that
	// the hub found no repeater for.
	NotRouted(stream, dst uint)
	// QueueHeard is called when the hub hears a radio on a repeater that
	// private traffic to it may go to. It must not block.
	QueueHeard(radio, repeater uint, slot bool)
}

type storeForwardBox struct{ sf StoreForward }

type tapKey struct{ src, dst uint }

type dataSlot struct {
	repeater uint
	slot     bool
}

// SetStoreForward sets what the hub tells of the private data that it
// cannot route and the radios that it hears.
func (h *Hub) SetStoreForward(sf StoreForward) {
	h.storeForward.Store(&storeForwardBox{sf: sf})
}

func (h *Hub) notRouted(packet models.Packet) {
	b := h.storeForward.Load()
	if b == nil || packet.GroupCall {
		return
	}
	if isVoice, _ := utils.CheckPacketType(packet); isVoice {
		return
	}
	b.sf.NotRouted(packet.StreamID, packet.Dst)
}

func (h *Hub) heardForStoreForward(s sighting) {
	if b := h.storeForward.Load(); b != nil {
		b.sf.QueueHeard(s.src, s.repeater, s.slot)
	}
}

// LastBurst returns when this replica last routed a burst from radio.
func (h *Hub) LastBurst(radio uint) (time.Time, bool) {
	heard, ok := h.heardOn.Load(radio)
	return heard.at, ok
}

// noteIngress records when a burst of a radio from sourceName, a server
// of repeaters, reached the timeslot of its repeater.
func (h *Hub) noteIngress(packet models.Packet, sourceName string) {
	if packet.Repeater == 0 || h.getServerRole(sourceName) != RoleRepeater {
		return
	}
	h.slotIngress.Store(repeaterSlot{repeater: packet.Repeater, slot: packet.Slot}, h.clock.Now())
}

// heardAfter reports whether this replica routed a burst from the timeslot
// key after at.
func (h *Hub) heardAfter(key repeaterSlot, at time.Time) bool {
	heard, ok := h.slotIngress.Load(key)
	return ok && heard.After(at)
}

func (h *Hub) tapData(packet models.Packet) {
	if packet.GroupCall || packet.FrameType != dmrconst.FrameDataSync {
		return
	}
	if ch, ok := h.dataTaps.Load(tapKey{src: packet.Src, dst: packet.Dst}); ok {
		select {
		case ch <- packet:
		default:
		}
	}
}

// SendData sends the data message m to the repeater repeaterID on slot
// (true for timeslot 2) as the network side of the repeater, from the
// source and to the radio of its header, and returns how it ended: sent,
// or for a confirmed message acknowledged, refused or not answered. The
// responses of the radio come from the bursts that this replica routes.
// It returns when ctx ends or the hub stops, with data.ResultCancelled.
// The messages to one timeslot of a repeater go one after another, as a
// radio takes one packet at a time, each once the timeslot carried no
// burst for the hang of the prompts. Voice goes first: a call that
// reaches the timeslot while a burst of m is due stops m, which goes again
// in full once the timeslot is quiet. So does a burst of a radio that
// reaches the timeslot during a try of m, after its first burst. A call
// while m waits for the answer of the radio does not stop it.
func (h *Hub) SendData(ctx context.Context, repeaterID uint, slot bool, m data.Message) (data.Result, error) {
	busy, _ := h.dataSlots.LoadOrStore(dataSlot{repeater: repeaterID, slot: slot}, make(chan struct{}, 1))
	select {
	case busy <- struct{}{}:
	case <-ctx.Done():
		return data.ResultCancelled, nil
	case <-h.done:
		return data.ResultCancelled, nil
	}
	defer func() { <-busy }()
	for {
		if !h.waitQuietSlot(ctx, repeaterID, slot) {
			return data.ResultCancelled, nil
		}
		r, stopped, err := h.sendDataOnce(ctx, repeaterID, slot, m)
		if !stopped {
			return r, err
		}
	}
}

// sendDataOnce sends m once, and reports stopped when a call reached the
// timeslot while a burst of m was due.
func (h *Hub) sendDataOnce(ctx context.Context, repeaterID uint, slot bool, m data.Message) (data.Result, bool, error) {
	rpt, err := h.routingRepeater(repeaterID)
	if err != nil {
		return 0, false, fmt.Errorf("finding repeater %d: %w", repeaterID, err)
	}
	dst, src, ok := m.Header.Addresses()
	if !ok {
		return 0, false, errNoAddresses
	}
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	es := enums.Slot1
	if slot {
		es = enums.Slot2
	}
	tx, err := data.NewSender(&data.SenderConfig{
		ColorCode: enums.ColorCode(rpt.ColorCode & 0xF), Source: layer2.SyncSourceBS, Access: enums.AccessImpolite,
		ResponseWait: storeForwardResponseWait, Retries: storeForwardRetries, Slot: es, Seed: binary.BigEndian.Uint64(seed[:]),
	})
	if err != nil {
		return 0, false, fmt.Errorf("making a data sender: %w", err)
	}
	tap := make(chan models.Packet, tapDepth)
	key := tapKey{src: uint(dst), dst: uint(src)}
	h.dataTaps.Store(key, tap)
	defer h.dataTaps.Compute(key, func(old chan models.Packet, loaded bool) (chan models.Packet, xsync.ComputeOp) {
		if loaded && old == tap {
			return nil, xsync.DeleteOp
		}
		return old, xsync.CancelOp
	})

	start := h.clock.Now()
	since := func() time.Duration { return h.clock.Since(start) }
	if err := tx.Send(0, &m); err != nil {
		return 0, false, fmt.Errorf("sending a data message: %w", err)
	}
	tx.Channel(0, false)
	out := &sfOutput{h: h, repeater: repeaterID, slot: slot, src: uint(src), dst: uint(dst), stream: newStreamID()}
	h.hubStreams.Store(out.stream, struct{}{})
	defer h.hubStreams.Delete(out.stream)
	rs := repeaterSlot{repeater: repeaterID, slot: slot}
	outs := make([]data.Output, 0, data.MaxOutputs)
	var try dataTry
	for {
		outs = tx.Tick(since(), outs[:0])
		if h.givesWay(rs, start, &try, outs) {
			return 0, true, nil
		}
		if r, done := out.emit(outs); done {
			return r, false, nil
		}
		at, ok := tx.Deadline()
		if !ok {
			return data.ResultCancelled, false, nil
		}
		timer := h.clock.NewTimer(max(at-since(), 0))
		select {
		case <-timer.C():
		case p := <-tap:
			timer.Stop()
			raw := layer2.RawBurst(p.DMRData)
			outs = tx.Burst(since(), &raw, layer2.Hint{Kind: layer2.KindData}, outs[:0])
			if h.givesWay(rs, start, &try, outs) {
				return 0, true, nil
			}
			if r, done := out.emit(outs); done {
				return r, false, nil
			}
		case <-ctx.Done():
			timer.Stop()
			return data.ResultCancelled, false, nil
		case <-h.done:
			timer.Stop()
			return data.ResultCancelled, false, nil
		}
	}
}

// dataTry is the try of a data message that the hub sends, and when its
// first burst went out.
type dataTry struct {
	n  uint8
	at time.Time
}

// givesWay reports whether outs, the next outputs of a data message that
// the hub sends to the timeslot key since start, have a burst while the
// timeslot carries other traffic: a voice burst that the hub did not make
// since start, or a burst of a radio after the first burst of the try. The
// response of the radio, which comes before the try that it asks for, does
// not count. It records the start of each try in try.
func (h *Hub) givesWay(key repeaterSlot, start time.Time, try *dataTry, outs []data.Output) bool {
	for i := range outs {
		if outs[i].Kind != data.OutputBurst {
			continue
		}
		if outs[i].Try != try.n {
			try.n, try.at = outs[i].Try, h.clock.Now()
		}
		return h.voiceSince(key, start) || h.heardAfter(key, try.at)
	}
	return false
}

// voiceSince reports whether the hub sent a voice burst of a call that it
// did not make itself to the timeslot key at or after since.
func (h *Hub) voiceSince(key repeaterSlot, since time.Time) bool {
	at, ok := h.slotVoice.Load(key)
	return ok && !at.Before(since)
}

type sfOutput struct {
	h        *Hub
	repeater uint
	slot     bool
	src, dst uint
	stream   uint
	seq      uint
}

func (o *sfOutput) emit(outs []data.Output) (data.Result, bool) {
	for i := range outs {
		switch outs[i].Kind {
		case data.OutputBurst:
			raw := outs[i].Burst
			p := models.Packet{
				Signature: string(dmrconst.CommandDMRD), Seq: o.seq & 0xFF, Src: o.src, Dst: o.dst, Slot: o.slot,
				FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(raw.SlotType().DataType), StreamID: o.stream,
				DMRData: raw, BER: -1, RSSI: -1,
			}
			o.seq++
			key := sourceSlot{streamSource: streamSource{server: storeForwardSource}, slot: o.slot}
			o.h.publishToRepeater(key, o.repeater, p)
		case data.OutputDone:
			return outs[i].Result, true
		}
	}
	return 0, false
}

func newStreamID() uint {
	var b [4]byte
	for {
		_, _ = rand.Read(b[:])
		if id := binary.BigEndian.Uint32(b[:]); id != 0 {
			return uint(id)
		}
	}
}
