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
// burst for the hang of the prompts.
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
	if !h.waitQuietSlot(ctx, repeaterID, slot) {
		return data.ResultCancelled, nil
	}
	rpt, err := h.routingRepeater(repeaterID)
	if err != nil {
		return 0, fmt.Errorf("finding repeater %d: %w", repeaterID, err)
	}
	dst, src, ok := m.Header.Addresses()
	if !ok {
		return 0, errNoAddresses
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
		return 0, fmt.Errorf("making a data sender: %w", err)
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
		return 0, fmt.Errorf("sending a data message: %w", err)
	}
	tx.Channel(0, false)
	out := &sfOutput{h: h, repeater: repeaterID, slot: slot, src: uint(src), dst: uint(dst), stream: newStreamID()}
	outs := make([]data.Output, 0, data.MaxOutputs)
	for {
		outs = tx.Tick(since(), outs[:0])
		if r, done := out.emit(outs); done {
			return r, nil
		}
		at, ok := tx.Deadline()
		if !ok {
			return data.ResultCancelled, nil
		}
		timer := h.clock.NewTimer(max(at-since(), 0))
		select {
		case <-timer.C():
		case p := <-tap:
			timer.Stop()
			raw := layer2.RawBurst(p.DMRData)
			outs = tx.Burst(since(), &raw, layer2.Hint{Kind: layer2.KindData}, outs[:0])
			if r, done := out.emit(outs); done {
				return r, nil
			}
		case <-ctx.Done():
			timer.Stop()
			return data.ResultCancelled, nil
		case <-h.done:
			timer.Stop()
			return data.ResultCancelled, nil
		}
	}
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
