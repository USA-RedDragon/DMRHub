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

// Package prompts holds the spoken prompts that the hub plays to a radio,
// such as when it refuses a transmission, and builds the calls that carry
// them. Each voice set holds every clip twice, as AMBE+2 and as Codec2
// vocoder frames, made once by a generator outside DMRHub, so that the hub
// needs no AMBE+2 encoder.
package prompts

import (
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
)

// The names of the clips.
const (
	TalkgroupNotFound    = "talkgroup-not-found"
	TalkgroupRestricted  = "talkgroup-restricted"
	TalkgroupBlocked     = "talkgroup-blocked"
	TimeslotReserved     = "timeslot-reserved"
	RadioIDNotRegistered = "radio-id-not-registered"
	ChannelBusy          = "channel-busy"
	TimeOut              = "time-out"
	CodeNeeded           = "code-needed"
	Unlocked             = "unlocked"
	WrongCode            = "wrong-code"
	WordTalkgroup        = "word-talkgroup"
	WordTimeslot         = "word-timeslot"
	WordLinked           = "word-linked"
	WordStatic           = "word-static"
	WordDynamic          = "word-dynamic"
	WordNone             = "word-none"
)

// Text returns the words of the clip, or "" for a clip that the package
// does not have.
func Text(clip string) string {
	switch clip {
	case TalkgroupNotFound:
		return "Talkgroup not found."
	case TalkgroupRestricted:
		return "Talkgroup restricted."
	case TalkgroupBlocked:
		return "Talkgroup blocked here."
	case TimeslotReserved:
		return "Timeslot reserved."
	case RadioIDNotRegistered:
		return "Radio ID not registered."
	case ChannelBusy:
		return "Channel busy."
	case TimeOut:
		return "Time out."
	case CodeNeeded:
		return "Code needed."
	case Unlocked:
		return "Unlocked."
	case WrongCode:
		return "Wrong code."
	case WordTalkgroup:
		return "Talkgroup"
	case WordTimeslot:
		return "Timeslot"
	case WordLinked:
		return "Linked"
	case WordStatic:
		return "Static"
	case WordDynamic:
		return "Dynamic"
	case WordNone:
		return "None"
	}
	digits := [...]string{"Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine"}
	for d, word := range digits {
		if clip == Digit(d) {
			return word
		}
	}
	return ""
}

// Digit returns the name of the clip of the digit d, 0 to 9.
func Digit(d int) string {
	return fmt.Sprintf("digit-%d", d)
}

// DefaultVoice is the voice set that the hub uses when no other is set.
const DefaultVoice = "kitten-mini-luna"

// DefaultServiceID is the source ID of the prompts when Setup sets none.
const DefaultServiceID = 9999

// ErrUnknown is wrapped by the errors for a voice, clip or codec that the
// package does not have.
var ErrUnknown = errors.New("unknown prompt")

const slotBytes = 9

//go:embed voices
var voices embed.FS

// Voice is a voice set.
type Voice struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	About string `json:"about"`
}

// Clips returns the names of the clips that every voice set holds.
func Clips() []string {
	c := []string{
		TalkgroupNotFound, TalkgroupRestricted, TalkgroupBlocked, TimeslotReserved,
		RadioIDNotRegistered, ChannelBusy, TimeOut, CodeNeeded, Unlocked, WrongCode,
	}
	for d := range 10 {
		c = append(c, Digit(d))
	}
	return append(c, WordTalkgroup, WordTimeslot, WordLinked, WordStatic, WordDynamic, WordNone)
}

// Voices returns the voice sets, by ID.
func Voices() []Voice {
	dirs, err := fs.ReadDir(voices, "voices")
	if err != nil {
		return nil
	}
	out := make([]Voice, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		v := Voice{ID: d.Name(), Name: d.Name()}
		if b, err := voices.ReadFile("voices/" + d.Name() + "/voice.json"); err == nil {
			_ = json.Unmarshal(b, &v)
			v.ID = d.Name()
		}
		out = append(out, v)
	}
	return out
}

// HasVoice reports whether id names a voice set.
func HasVoice(id string) bool {
	return slices.ContainsFunc(Voices(), func(v Voice) bool { return v.ID == id })
}

// Frames returns the vocoder slots of the clip of the voice set in the
// codec c, a whole number of voice bursts.
func Frames(voiceID, clip string, c transcode.Codec) ([][slotBytes]byte, error) {
	var ext string
	switch c {
	case transcode.CodecAMBE:
		ext = ".ambe"
	case transcode.CodecCodec2:
		ext = ".codec2"
	case transcode.CodecUnknown:
		return nil, fmt.Errorf("%w: codec %d", ErrUnknown, c)
	default:
		return nil, fmt.Errorf("%w: codec %d", ErrUnknown, c)
	}
	if !HasVoice(voiceID) || !slices.Contains(Clips(), clip) {
		return nil, fmt.Errorf("%w: %s %s", ErrUnknown, voiceID, clip)
	}
	b, err := voices.ReadFile("voices/" + voiceID + "/" + clip + ext)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", ErrUnknown, voiceID, clip, err)
	}
	if len(b) == 0 || len(b)%(slotBytes*layer2.VoiceSlotsPerBurst) != 0 {
		return nil, fmt.Errorf("prompt %s %s%s: %d bytes is not a whole number of bursts", voiceID, clip, ext, len(b))
	}
	slots := make([][slotBytes]byte, len(b)/slotBytes)
	for i := range slots {
		slots[i] = [slotBytes]byte(b[i*slotBytes:])
	}
	return slots, nil
}

// Call is one prompt played as a private voice call from Src to Dst on a
// slot of the repeater Repeater: the clip Clip, or the clips Clips one
// after another when there are any.
type Call struct {
	Voice     string
	Clip      string
	Clips     []string
	Codec     transcode.Codec
	Src, Dst  uint
	Repeater  uint
	Slot2     bool
	ColorCode enums.ColorCode
	Stream    uint
}

const burstTime = 60 * time.Millisecond

// Build returns the bursts of the call c: the Voice LC Header, the voice
// superframes of its clips and the Terminator with LC. The last superframe
// is padded with the last frame of the clip, which is silence, so that a
// Codec2 call holds no AMBE+2 silence.
func Build(c Call) ([]models.Packet, error) {
	clips := c.Clips
	if len(clips) == 0 {
		clips = []string{c.Clip}
	}
	var slots [][slotBytes]byte
	for _, clip := range clips {
		s, err := Frames(c.Voice, clip, c.Codec)
		if err != nil {
			return nil, err
		}
		slots = append(slots, s...)
	}
	slot := enums.Slot1
	if c.Slot2 {
		slot = enums.Slot2
	}
	cc := c.ColorCode
	if cc > 15 {
		cc = 1
	}
	tx, err := voice.NewTransmitter(&voice.TransmitterConfig{
		ColorCode: cc, Source: layer2.SyncSourceBS, Slot: slot, Silence: slots[len(slots)-1], Timeout: -1,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot make a voice transmitter: %w", err)
	}
	call := voice.Call{Target: uint32(c.Dst & 0xFFFFFF), Source: uint32(c.Src & 0xFFFFFF), FID: enums.FIDStandardized}
	if c.Codec == transcode.CodecCodec2 {
		call.FID = transcode.Codec2FID
	}
	if err := tx.Start(0, call, nil); err != nil {
		return nil, fmt.Errorf("cannot start the call: %w", err)
	}
	out := make([]models.Packet, 0, len(slots)/layer2.VoiceSlotsPerBurst+8)
	outputs := make([]voice.Output, 0, voice.MaxOutputs)
	stopped := false
	var now time.Duration
	for {
		for len(slots) > 0 && tx.Room() > 0 {
			if err := tx.VocoderSlot(slots[0]); err != nil {
				return nil, fmt.Errorf("cannot queue a frame: %w", err)
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
				Seq: uint8(len(out) & 0xFF), Source: call.Source, Destination: call.Target, //nolint:gosec // masked to 8 bits
				Repeater: uint32(c.Repeater), Slot: slot, Stream: uint32(c.Stream), Burst: o.Burst, //nolint:gosec // a repeater ID and a stream ID are 32 bits
			}
			if idx, ok := o.Hint.VoiceBurst.Index(); o.Hint.Kind == layer2.KindVoice && ok {
				d.Frame, d.Value = dmrd.FrameVoice, uint8(idx) //nolint:gosec // 0 to 5
				if idx == 0 {
					d.Frame = dmrd.FrameVoiceSync
				}
			} else {
				d.Frame, d.Value = dmrd.FrameDataSync, uint8(o.Hint.DataType)
			}
			p := models.PacketFromData(&d, false)
			p.Codec = uint8(c.Codec)
			out = append(out, p)
		}
	}
}

// Duration returns how long the bursts of a call take on the air.
func Duration(bursts int) time.Duration {
	return time.Duration(bursts) * burstTime
}

// Preview returns the clip of the voice set as a WAV file of 8 kHz mono
// 16-bit PCM, decoded from its Codec2 frames, so that a browser can play it
// with no AMBE+2 decoder.
func Preview(voiceID, clip string) ([]byte, error) {
	slots, err := Frames(voiceID, clip, transcode.CodecCodec2)
	if err != nil {
		return nil, err
	}
	dec, err := codec2.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cannot make a Codec2 decoder: %w", err)
	}
	defer func() { _ = dec.Close() }()
	pcm := make([]int16, 0, len(slots)*dec.FrameSamples())
	for _, s := range slots {
		if pcm, _, err = dec.Decode(pcm, s); err != nil {
			return nil, fmt.Errorf("cannot decode the prompt: %w", err)
		}
	}
	return wav(pcm), nil
}

func wav(pcm []int16) []byte {
	const rate = 8000
	n := uint32(2 * len(pcm)) //nolint:gosec // a clip is seconds long
	b := make([]byte, 0, 44+n)
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, 36+n)
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint32(b, rate)
	b = binary.LittleEndian.AppendUint32(b, 2*rate)
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, n)
	for _, s := range pcm {
		b = binary.LittleEndian.AppendUint16(b, uint16(s)) //nolint:gosec // the bits of the sample
	}
	return b
}
