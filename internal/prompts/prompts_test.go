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

package prompts_test

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEveryVoiceHasEveryClipInBothCodecsAsWholeBursts(t *testing.T) {
	t.Parallel()
	voices := prompts.Voices()
	require.NotEmpty(t, voices)
	assert.True(t, prompts.HasVoice(prompts.DefaultVoice))
	for _, v := range voices {
		assert.NotEmpty(t, v.Name, v.ID)
		for _, clip := range prompts.Clips() {
			for _, c := range []transcode.Codec{transcode.CodecAMBE, transcode.CodecCodec2} {
				slots, err := prompts.Frames(v.ID, clip, c)
				require.NoError(t, err, "%s %s %v", v.ID, clip, c)
				assert.NotEmpty(t, slots)
				assert.Zero(t, len(slots)%3, "%s %s: a whole number of bursts", v.ID, clip)
			}
		}
	}
}

func TestFramesRefusesAnUnknownVoiceClipOrCodec(t *testing.T) {
	t.Parallel()
	_, err := prompts.Frames("no-such-voice", prompts.ChannelBusy, transcode.CodecAMBE)
	require.ErrorIs(t, err, prompts.ErrUnknown)
	_, err = prompts.Frames(prompts.DefaultVoice, "no-such-clip", transcode.CodecAMBE)
	require.ErrorIs(t, err, prompts.ErrUnknown)
	_, err = prompts.Frames(prompts.DefaultVoice, prompts.ChannelBusy, transcode.CodecUnknown)
	require.ErrorIs(t, err, prompts.ErrUnknown)
}

func receive(t *testing.T, packets []models.Packet) (slots [][9]byte, calls []voice.Call) {
	t.Helper()
	rx, err := voice.NewReceiver(&voice.ReceiverConfig{Lost: -1, Hang: -1})
	require.NoError(t, err)
	events := make([]voice.Event, 0, voice.MaxEvents)
	for i := range packets {
		d := packets[i].Data()
		raw := d.Burst
		now := time.Duration(i) * 60 * time.Millisecond
		events = rx.Burst(now, &raw, d.Hint(now), events[:0])
		for j := range events {
			e := &events[j]
			if e.Kind == voice.EventVoice {
				s := e.Burst.VoiceSlots()
				slots = append(slots, s[:]...)
			} else if e.Call.Target != 0 {
				calls = append(calls, e.Call)
			}
		}
	}
	return slots, calls
}

func TestACallIsAPrivateCallThatCarriesTheClipInTheCodecOfTheTarget(t *testing.T) {
	t.Parallel()
	for _, c := range []transcode.Codec{transcode.CodecAMBE, transcode.CodecCodec2} {
		call := prompts.Call{Voice: prompts.DefaultVoice, Clip: prompts.TalkgroupNotFound, Codec: c, Src: 9999, Dst: 3000001, Repeater: 990001, Slot2: true, ColorCode: 3, Stream: 77}
		packets, err := prompts.Build(call)
		require.NoError(t, err)
		require.NotEmpty(t, packets)
		first, last := packets[0], packets[len(packets)-1]
		assert.Equal(t, dmrconst.FrameDataSync, first.FrameType)
		assert.Equal(t, uint(dmrconst.DTypeVoiceHead), first.DTypeOrVSeq)
		assert.Equal(t, dmrconst.FrameDataSync, last.FrameType)
		assert.Equal(t, uint(dmrconst.DTypeVoiceTerm), last.DTypeOrVSeq)
		for i, p := range packets {
			assert.False(t, p.GroupCall)
			assert.Equal(t, uint(9999), p.Src)
			assert.Equal(t, uint(3000001), p.Dst)
			assert.Equal(t, uint(990001), p.Repeater)
			assert.True(t, p.Slot)
			assert.Equal(t, uint(77), p.StreamID)
			assert.Equal(t, uint(i&0xFF), p.Seq)
			assert.Equal(t, uint8(c), p.Codec)
		}
		want, err := prompts.Frames(prompts.DefaultVoice, prompts.TalkgroupNotFound, c)
		require.NoError(t, err)
		slots, calls := receive(t, packets)
		require.GreaterOrEqual(t, len(slots), len(want))
		assert.Equal(t, want, slots[:len(want)], "the clip, in order")
		require.NotEmpty(t, calls)
		assert.False(t, calls[0].Group)
		assert.Equal(t, uint32(3000001), calls[0].Target)
		assert.Equal(t, uint32(9999), calls[0].Source)
		if c == transcode.CodecCodec2 {
			assert.Equal(t, transcode.Codec2FID, calls[0].FID)
			for _, s := range slots[len(want):] {
				assert.Equal(t, want[len(want)-1], s, "a Codec2 call is padded with its last frame, not AMBE silence")
			}
		} else {
			assert.Equal(t, enums.FIDStandardized, calls[0].FID)
		}
	}
}

func TestBuildRefusesAnUnknownClip(t *testing.T) {
	t.Parallel()
	_, err := prompts.Build(prompts.Call{Voice: prompts.DefaultVoice, Clip: "nope", Codec: transcode.CodecAMBE, Src: 9999, Dst: 3000001})
	require.ErrorIs(t, err, prompts.ErrUnknown)
}

func TestPreviewIsAnEightKilohertzWAVOfTheCodec2Copy(t *testing.T) {
	t.Parallel()
	b, err := prompts.Preview(prompts.DefaultVoice, prompts.Unlocked)
	require.NoError(t, err)
	require.Greater(t, len(b), 44)
	assert.True(t, bytes.HasPrefix(b, []byte("RIFF")))
	assert.Equal(t, []byte("WAVE"), b[8:12])
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(b[22:]), "mono")
	assert.Equal(t, uint32(8000), binary.LittleEndian.Uint32(b[24:]))
	slots, err := prompts.Frames(prompts.DefaultVoice, prompts.Unlocked, transcode.CodecCodec2)
	require.NoError(t, err)
	assert.Equal(t, uint32(2*160*len(slots)), binary.LittleEndian.Uint32(b[40:]))
	var energy float64
	for i := 44; i+1 < len(b); i += 2 {
		s := float64(int16(binary.LittleEndian.Uint16(b[i:])))
		energy += s * s
	}
	assert.Positive(t, energy, "the preview is not silent")
	_, err = prompts.Preview("no-such-voice", prompts.Unlocked)
	require.ErrorIs(t, err, prompts.ErrUnknown)
}

func TestTheServiceIDIsNoIDTheHubAlreadyUses(t *testing.T) {
	t.Parallel()
	for _, id := range []uint{9990, 4000, 5000, 5050, 5059} {
		assert.NotEqual(t, id, uint(prompts.DefaultServiceID))
	}
	assert.False(t, dmrconst.IsUnlockID(prompts.DefaultServiceID))
}

func TestEveryClipHasItsWords(t *testing.T) {
	t.Parallel()
	for _, clip := range prompts.Clips() {
		assert.NotEmpty(t, prompts.Text(clip), clip)
	}
	assert.Empty(t, prompts.Text("no-such-clip"))
	assert.Equal(t, "Wrong code.", prompts.Text(prompts.WrongCode))
}

func TestACallCarriesItsClipsOneAfterAnother(t *testing.T) {
	t.Parallel()
	clips := []string{prompts.WordTimeslot, prompts.Digit(2), prompts.WordDynamic, prompts.WordNone}
	for _, c := range []transcode.Codec{transcode.CodecAMBE, transcode.CodecCodec2} {
		packets, err := prompts.Build(prompts.Call{Voice: prompts.DefaultVoice, Clips: clips, Codec: c, Src: 5000, Dst: 3000001, Repeater: 990001, Stream: 78})
		require.NoError(t, err)
		var want [][9]byte
		for _, clip := range clips {
			f, err := prompts.Frames(prompts.DefaultVoice, clip, c)
			require.NoError(t, err)
			want = append(want, f...)
		}
		slots, _ := receive(t, packets)
		require.GreaterOrEqual(t, len(slots), len(want))
		assert.Equal(t, want, slots[:len(want)], "the clips, in order")
	}
	_, err := prompts.Build(prompts.Call{Voice: prompts.DefaultVoice, Clips: []string{prompts.WordTimeslot, "nope"}, Codec: transcode.CodecAMBE, Src: 5000, Dst: 3000001})
	require.ErrorIs(t, err, prompts.ErrUnknown)
}
