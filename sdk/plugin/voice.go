// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package plugin

import (
	"context"
	"errors"
	"fmt"
	"time"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
)

// BurstTime is the time of one DMR burst. A plugin sends the bursts of a
// call one BurstTime apart, as Play does.
const BurstTime = 60 * time.Millisecond

const maxID = 1<<24 - 1

// ErrVoiceCall is returned by VoiceBursts for a call that DMR cannot carry.
var ErrVoiceCall = errors.New("invalid voice call")

// VoiceCall is a voice call that a plugin sends.
type VoiceCall struct {
	// Source is one of the plugin's IDs.
	Source uint32
	// Destination is a radio, or a talkgroup among the plugin's IDs when
	// Group is true.
	Destination uint32
	Group       bool
	// Slot is the timeslot, 1 or 2.
	Slot uint32
	// Stream is the stream ID of the call, not 0, and new for each call.
	Stream uint32
	// ColorCode is the color code of the bursts, 0 to 15.
	ColorCode uint8
}

// VoiceBursts returns the bursts of the voice call c that plays slots, the
// 72-bit AMBE+2 vocoder slots in their order, such as the slots of a few
// clips one after another: a Voice LC Header, the voice superframes with
// the embedded LC, and a Terminator with LC. AMBE+2 silence pads the last
// superframe. Send the bursts with Play.
func VoiceBursts(c VoiceCall, slots [][9]byte) ([]*pluginv1.Burst, error) {
	if c.Source == 0 || c.Source > maxID || c.Destination == 0 || c.Destination > maxID || c.Stream == 0 || c.ColorCode > 15 ||
		(c.Slot != 1 && c.Slot != 2) || len(slots) == 0 {
		return nil, ErrVoiceCall
	}
	slot := enums.Slot1
	if c.Slot == 2 {
		slot = enums.Slot2
	}
	tx, err := voice.NewTransmitter(&voice.TransmitterConfig{
		ColorCode: enums.ColorCode(c.ColorCode), Source: layer2.SyncSourceBS, Slot: slot, Silence: vocoder.AMBESilenceSlot(), Timeout: -1,
	})
	if err != nil {
		return nil, fmt.Errorf("could not make a voice transmitter: %w", err)
	}
	if err := tx.Start(0, voice.Call{Group: c.Group, Target: c.Destination, Source: c.Source}, nil); err != nil {
		return nil, fmt.Errorf("could not start the call: %w", err)
	}
	out := make([]*pluginv1.Burst, 0, len(slots)/layer2.VoiceSlotsPerBurst+4)
	outputs := make([]voice.Output, 0, voice.MaxOutputs)
	stopped := false
	var now time.Duration
	for {
		for len(slots) > 0 && tx.Room() > 0 {
			if err := tx.VocoderSlot(slots[0]); err != nil {
				return nil, fmt.Errorf("could not queue the voice: %w", err)
			}
			slots = slots[1:]
		}
		if len(slots) == 0 && !stopped {
			tx.Stop(now)
			stopped = true
		}
		at, ok := tx.Deadline()
		if !ok {
			return out, nil
		}
		now = at
		outputs = tx.Tick(at, outputs[:0])
		for i := range outputs {
			o := &outputs[i]
			if o.Kind == voice.OutputEnded {
				return out, nil
			}
			d := dmrd.Data{
				Seq: uint8(len(out) & 0xFF), Source: c.Source, Destination: c.Destination, Slot: slot, Group: c.Group, //nolint:gosec // masked to 8 bits
				Stream: c.Stream, Burst: o.Burst,
			}
			if idx, ok := o.Hint.VoiceBurst.Index(); o.Hint.Kind == layer2.KindVoice && ok {
				d.Frame, d.Value = dmrd.FrameVoice, uint8(idx) //nolint:gosec // 0 to 5
				if idx == 0 {
					d.Frame = dmrd.FrameVoiceSync
				}
			} else {
				d.Frame, d.Value = dmrd.FrameDataSync, uint8(o.Hint.DataType)
			}
			out = append(out, BurstOf(&d))
		}
	}
}

// Play sends bursts with send, such as Conn.Send, one BurstTime apart, as
// fast as a timeslot carries them. It returns when it sent the last one,
// when send fails, or when ctx ends.
func Play(ctx context.Context, send func(*pluginv1.Burst) error, bursts []*pluginv1.Burst) error {
	t := time.NewTicker(BurstTime)
	defer t.Stop()
	for i, b := range bursts {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("the call stopped: %w", ctx.Err())
			case <-t.C:
			}
		}
		if err := send(b); err != nil {
			return err
		}
	}
	return nil
}
