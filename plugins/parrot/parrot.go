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

// Package parrot is a DMRHub plugin that plays each private voice call to
// its IDs back to the caller.
package parrot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
)

const (
	// burstTime is the time of one DMR burst.
	burstTime = 60 * time.Millisecond
	// replyDelay is how long the parrot waits after a call before it plays
	// the call back.
	replyDelay = 3 * time.Second
	// maxRecording is the most bursts of one call the parrot keeps, three
	// minutes of voice.
	maxRecording = 3000
	// forget is how long the parrot remembers a call it played back, so a
	// repeated terminator does not start another recording.
	forget = time.Minute
	// idle is how long the parrot keeps a call that gets no bursts.
	idle = 5 * time.Minute
	// maxDMRAddress is the largest 24-bit DMR ID.
	maxDMRAddress = 1<<24 - 1
)

// Parrot records the private voice calls to its IDs and plays each back to
// its caller as a new call.
type Parrot struct {
	send  func(*pluginv1.Burst) error
	delay time.Duration
	sleep func(time.Duration)
	now   func() time.Time

	mu      sync.Mutex
	calls   map[uint32]*recording
	ended   map[uint32]time.Time
	streams uint32
	wg      sync.WaitGroup
}

// recording is a call that the parrot is recording.
type recording struct {
	bursts []dmrd.Data
	last   time.Time
}

// NewParrot returns a Parrot that sends its replies with send.
func NewParrot(send func(*pluginv1.Burst) error) *Parrot {
	return &Parrot{
		send: send, delay: replyDelay, sleep: time.Sleep, now: time.Now,
		calls: map[uint32]*recording{}, ended: map[uint32]time.Time{},
		streams: uint32(time.Now().UnixNano() & math.MaxUint32), //nolint:gosec // masked to 32 bits
	}
}

// Wait waits for the replies being played back.
func (p *Parrot) Wait() {
	p.wg.Wait()
}

// Handle records b, a burst to one of the parrot's IDs, and starts the
// reply when b ends its call. It ignores group calls and data.
func (p *Parrot) Handle(b *pluginv1.Burst) {
	if b.GetGroupCall() || !plugin.IsVoice(b) {
		return
	}
	stream := b.GetStreamId()
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for s, at := range p.ended {
		if now.Sub(at) > forget {
			delete(p.ended, s)
		}
	}
	for s, rec := range p.calls {
		if now.Sub(rec.last) > idle {
			delete(p.calls, s)
		}
	}
	if _, done := p.ended[stream]; done {
		return
	}
	rec, started := p.calls[stream]
	end := plugin.IsTerminator(b)
	if !started {
		if end {
			return
		}
		rec = &recording{}
		p.calls[stream] = rec
	}
	rec.last = now
	if len(rec.bursts) < maxRecording {
		d := plugin.DataOf(b)
		d.Source, d.Destination, d.Private = b.GetDestination(), b.GetSource(), true
		rec.bursts = append(rec.bursts, d)
	}
	if !end {
		return
	}
	delete(p.calls, stream)
	p.ended[stream] = now
	p.streams++
	reply := Reply(rec.bursts, p.streams)
	p.wg.Go(func() { p.play(reply) })
}

func (p *Parrot) play(reply []dmrd.Data) {
	p.sleep(p.delay)
	for i := range reply {
		if err := p.send(plugin.BurstOf(&reply[i])); err != nil {
			slog.Warn("Parrot: cannot send the reply", "error", err)
			return
		}
		p.sleep(burstTime)
	}
}

// colorCode returns the color code of the first data sync burst of rec,
// or 1 when it has none.
func colorCode(rec []dmrd.Data) enums.ColorCode {
	for i := range rec {
		if rec[i].Frame == dmrd.FrameDataSync {
			return rec[i].Burst.SlotType().ColorCode
		}
	}
	return 1
}

// Reply returns the bursts that play back the recording rec, whose source
// and destination are already those of the reply, as the new call stream.
// A dmrgo voice.Receiver takes the voice out of the recording and a
// voice.Transmitter sends it again, so that the Voice LC Header, the
// embedded LC and the Terminator with LC name the reply. The LC of the reply
// is that of the recording readdressed, so its FID and Service Options
// pass. The last superframe of a call whose FID is not that of AMBE+2 is
// padded with the last recorded slot, because AMBE+2 silence is noise to
// another codec such as Codec2. When the recording holds no voice that the
// Receiver can place, Reply returns the recording as it is.
func Reply(rec []dmrd.Data, stream uint32) []dmrd.Data {
	if len(rec) == 0 {
		return rec
	}
	slots, recorded, known := voiceSlots(rec)
	if len(slots) == 0 {
		return rec
	}
	total, last := len(slots), slots[len(slots)-1]
	tx, err := voice.NewTransmitter(&voice.TransmitterConfig{
		ColorCode: colorCode(rec), Source: layer2.SyncSourceBS, Silence: vocoder.AMBESilenceSlot(), Timeout: -1,
	})
	if err != nil {
		slog.Error("Parrot: cannot make a voice transmitter", "error", err)
		return rec
	}
	first := rec[0]
	call := replyCall(&first, recorded, known)
	padLast := call.FID != enums.FIDStandardized && call.FID != enums.FIDMotorola
	if err := tx.Start(0, call, nil); err != nil {
		slog.Error("Parrot: cannot start the reply", "error", err)
		return rec
	}
	reply := make([]dmrd.Data, 0, len(rec)+len(slots)/layer2.VoiceSlotsPerBurst+6)
	outputs := make([]voice.Output, 0, voice.MaxOutputs)
	stopped, sent := false, 0
	for {
		for len(slots) > 0 && tx.Room() > 0 {
			if err := tx.VocoderSlot(slots[0]); err != nil {
				slog.Error("Parrot: cannot queue voice", "error", err)
				return rec
			}
			slots = slots[1:]
		}
		if len(slots) == 0 && !stopped {
			tx.Stop()
			stopped = true
		}
		at, ok := tx.Deadline()
		if !ok {
			return reply
		}
		outputs = tx.Tick(at, outputs[:0])
		for i := range outputs {
			out := &outputs[i]
			if out.Kind == voice.OutputEnded {
				return reply
			}
			d := first
			d.Seq, d.Stream, d.Repeater, d.Burst = uint8(len(reply)&0xFF), stream, 0, out.Burst //nolint:gosec // masked to 8 bits
			if idx, ok := out.Hint.VoiceBurst.Index(); out.Hint.Kind == layer2.KindVoice && ok {
				d.Frame, d.Value = dmrd.FrameVoice, uint8(idx) //nolint:gosec // 0 to 5
				if idx == 0 {
					d.Frame = dmrd.FrameVoiceSync
				}
				if padLast {
					d.Burst = padWith(out.Burst, sent, total, last)
				}
				sent += layer2.VoiceSlotsPerBurst
			} else {
				d.Frame, d.Value = dmrd.FrameDataSync, uint8(out.Hint.DataType)
			}
			reply = append(reply, d)
		}
	}
}

// replyCall returns the call of the reply, from the destination to the
// source of the recording, with the LC of the recorded call rec, when
// known, readdressed.
func replyCall(first *dmrd.Data, rec voice.Call, known bool) voice.Call {
	c := voice.Call{Target: first.Destination & maxDMRAddress, Source: first.Source & maxDMRAddress}
	if !known {
		return c
	}
	lc := rec.LC
	if err := lc.SetCall(false, c.Target, c.Source); err != nil {
		return c
	}
	c.Options, c.FID, c.LC = rec.Options, rec.FID, lc
	return c
}

// padWith returns burst with each of its slots from the sent-th on that the
// recording does not have, the total-th and later, set to last.
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

// voiceSlots returns the vocoder slots of the voice bursts of rec, in the
// order a voice.Receiver places them, and the call that the Receiver learned
// from a Voice LC Header, an embedded LC or a Terminator with LC, with known
// false when it learned none.
func voiceSlots(rec []dmrd.Data) (slots [][9]byte, call voice.Call, known bool) {
	rx, err := voice.NewReceiver(&voice.ReceiverConfig{Lost: -1, Hang: -1})
	if err != nil {
		slog.Error("Parrot: cannot make a voice receiver", "error", err)
		return nil, call, false
	}
	events := make([]voice.Event, 0, voice.MaxEvents)
	for i := range rec {
		d := rec[i]
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
				call, known = e.Call, true
			}
		}
	}
	return slots, call, known
}

// Serve plays back the calls that come on conn until it ends. Give it to
// plugin.Run.
func Serve(_ context.Context, conn *plugin.Conn) error {
	p := NewParrot(conn.Send)
	defer p.Wait()
	for {
		m, err := conn.Recv()
		if err != nil {
			return fmt.Errorf("cannot receive: %w", err)
		}
		switch msg := m.GetMessage().(type) {
		case *pluginv1.ConnectResponse_Burst:
			p.Handle(msg.Burst)
		case *pluginv1.ConnectResponse_Refusal:
			slog.Warn("Parrot: the hub refused a burst", "stream", msg.Refusal.GetStreamId(), "reason", msg.Refusal.GetReason())
		case *pluginv1.ConnectResponse_Ranges:
			slog.Info("Parrot: new IDs", "ids", msg.Ranges.GetRanges())
		case *pluginv1.ConnectResponse_Welcome:
		default:
			return errors.New("unknown message from the hub")
		}
	}
}
