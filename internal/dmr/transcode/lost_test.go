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

package transcode_test

import (
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/dvsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seqCall is call with the DMRD sequence number of each packet set, as
// every transport sets it.
func seqCall(t *testing.T, lc pdu.LCBytes, slots [][9]byte) []models.Packet {
	t.Helper()
	in := call(t, lc, slots)
	for i := range in {
		in[i].Seq = uint(i)
	}
	return in
}

// without returns in without the packets at the indexes drop.
func without(in []models.Packet, drop ...int) []models.Packet {
	out := make([]models.Packet, 0, len(in))
	for i := range in {
		skip := false
		for _, d := range drop {
			skip = skip || d == i
		}
		if !skip {
			out = append(out, in[i])
		}
	}
	return out
}

// outBurst is one voice burst of a copy, a converted one or a fill.
type outBurst struct {
	raw  layer2.RawBurst
	pos  uint
	seq  uint
	fill bool
}

// convertAll converts in and returns the voice bursts of the copy in the
// order they go out, the fills before the burst they come with, and the
// frame reports in order.
func convertAll(t *testing.T, st *transcode.Stream, in []models.Packet) ([]outBurst, []vocoder.FrameReport) {
	t.Helper()
	var out []outBurst
	var reps []vocoder.FrameReport
	for i := range in {
		r, ok := st.Convert(t.Context(), &in[i], deadline(st))
		require.True(t, ok, "packet %d has no copy", i)
		require.Zero(t, r.Missed, "packet %d", i)
		for _, f := range r.Fill {
			out = append(out, outBurst{raw: f.Burst, pos: f.Position, seq: f.Seq, fill: true})
		}
		reps = append(reps, r.Frames...)
		if transcode.IsVoice(&in[i]) {
			out = append(out, outBurst{raw: r.Burst, pos: in[i].DTypeOrVSeq, seq: in[i].Seq})
		} else {
			require.Empty(t, r.Fill, "packet %d", i)
		}
	}
	return out, reps
}

// requireTiming checks that the copy has one voice burst for each 60 ms
// of the call: the positions and sequence numbers follow each other.
func requireTiming(t *testing.T, out []outBurst, firstSeq uint) {
	t.Helper()
	for i, b := range out {
		require.Equal(t, uint(i%layer2.VoiceBurstsPerSuperframe), b.pos, "burst %d position", i)
		require.Equal(t, firstSeq+uint(i), b.seq, "burst %d sequence number", i)
	}
}

// TestALostBurstIsFilledFromTheDecoder converts an AMBE call that lost a
// burst, then two in a row. The copy gets a burst for each one lost, so
// that it keeps the 60 ms timing of the call, with the audio that the
// chip makes for lost frames: a repeat of the frame before for 2 frames,
// then comfort noise.
func TestALostBurstIsFilledFromTheDecoder(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	slots := ambeFrames(12 * layer2.VoiceSlotsPerBurst)
	full := seqCall(t, voiceLC(t, enums.FIDStandardized), slots)
	lostBursts := []int{2, 8, 9}
	in := without(full, 1+2, 1+8, 1+9)

	st, err := svc.NewStream(transcode.CodecAMBE, transcode.CodecCodec2, codec2.Mode2400)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	out, reps := convertAll(t, st, in)
	requireTiming(t, out, 1)
	require.Len(t, out, 12)

	enc, err := codec2.New(&codec2.Config{Mode: codec2.Mode2400})
	require.NoError(t, err)
	t.Cleanup(func() { _ = enc.Close() })
	var last [dvsi.FrameSamples]int16
	repeats := 0
	var want []vocoder.FrameReport
	for b := range out {
		lost := false
		for _, l := range lostBursts {
			lost = lost || l == b
		}
		assert.Equal(t, lost, out[b].fill, "burst %d", b)
		got := out[b].raw.VoiceSlots()
		for k := range got {
			pcm := last
			rep := vocoder.FrameReport{Frame: vocoder.FrameLost, Output: vocoder.OutputRepeat}
			if !lost {
				pcm, last, repeats = testutils.FakeAMBEDecode(slots[b*3+k]), testutils.FakeAMBEDecode(slots[b*3+k]), 0
				rep = vocoder.FrameReport{}
			} else if repeats++; repeats > 2 {
				pcm, rep.Output = [dvsi.FrameSamples]int16{}, vocoder.OutputSilence
			}
			exp, err := enc.Encode(pcm[:])
			require.NoError(t, err)
			require.Equal(t, exp, got[k], "burst %d frame %d", b, k)
			want = append(want, rep)
		}
	}
	assert.Equal(t, want, reps)
	assert.Equal(t, 9, srv.LostFrames())
	assert.Equal(t, len(slots)-9, srv.Decodes())
	assert.Empty(t, srv.Errors())
}

// TestALostCodec2BurstIsFilled converts a Codec2 call that lost a burst.
// The AMBE copy gets a burst for it, encoded from the silence that
// libcodec2 puts in place of a lost frame.
func TestALostCodec2BurstIsFilled(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	slots := codec2Frames(t, codec2.Mode2400, 6*layer2.VoiceSlotsPerBurst)
	in := without(seqCall(t, voiceLC(t, transcode.Codec2FID), slots), 1+4)

	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	out, reps := convertAll(t, st, in)
	requireTiming(t, out, 1)
	require.Len(t, out, 6)
	require.True(t, out[4].fill)

	pcm := srv.EncodedPCM()
	require.Len(t, pcm, len(slots))
	for k, s := range out[4].raw.VoiceSlots() {
		assert.Equal(t, [dvsi.FrameSamples]int16{}, pcm[4*3+k], "frame %d", k)
		assert.Equal(t, testutils.FakeAMBEEncode(pcm[4*3+k][:]), s, "frame %d", k)
	}
	require.Len(t, reps, len(slots))
	for i, r := range reps {
		if i/3 == 4 {
			assert.Equal(t, vocoder.FrameReport{Frame: vocoder.FrameLost, Output: vocoder.OutputSilence}, r, "frame %d", i)
			continue
		}
		assert.Equal(t, vocoder.FrameReport{CountsKnown: true}, r, "frame %d", i)
	}
	assert.Empty(t, srv.Errors())
}

// TestAFillIsAVoiceBurstOfTheCall checks the bursts that fill a lost burst
// A and lost bursts C and D: burst A carries the voice SYNC of the call,
// and C and D the EMB of the call and the embedded LC of the copy, so the
// superframe still carries the LC.
func TestAFillIsAVoiceBurstOfTheCall(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	slots := ambeFrames(18 * layer2.VoiceSlotsPerBurst)
	full := seqCall(t, voiceLC(t, enums.FIDStandardized), slots)
	in := without(full, 1+6, 1+14, 1+15)

	st, err := svc.NewStream(transcode.CodecAMBE, transcode.CodecCodec2, codec2.Mode2400)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	out, _ := convertAll(t, st, in)
	requireTiming(t, out, 1)
	require.Len(t, out, 18)

	a := out[6]
	require.True(t, a.fill)
	gotSync, _ := a.raw.Sync()
	wantSync, _ := layer2.RawBurst(full[1+6].DMRData).Sync()
	assert.Equal(t, wantSync, gotSync, "burst A SYNC")
	for _, b := range []int{14, 15} {
		require.True(t, out[b].fill)
		orig := layer2.RawBurst(full[1+b].DMRData).EMB()
		got := out[b].raw.EMB()
		assert.Equal(t, orig.ColorCode, got.ColorCode, "burst %d color code", b)
		assert.Equal(t, orig.LCSS, got.LCSS, "burst %d LCSS", b)
	}
	var f [4][4]byte
	for k := range 4 {
		f[k] = out[13+k].raw.EmbeddedData()
	}
	lc, res := layer2.DecodeEmbeddedLC(f)
	require.False(t, res.Uncorrectable)
	assert.Equal(t, voiceLC(t, transcode.Codec2FID), pdu.LCBytes(lc))
}

// TestFillsAreCapped loses more bursts in a row than MaxFill. The copy
// fills the last MaxFill of them, right before the burst that came.
func TestFillsAreCapped(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	slots := ambeFrames(12 * layer2.VoiceSlotsPerBurst)
	in := without(seqCall(t, voiceLC(t, enums.FIDStandardized), slots), 1+2, 1+3, 1+4, 1+5)

	st, err := svc.NewStream(transcode.CodecAMBE, transcode.CodecCodec2, codec2.Mode2400)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	out, _ := convertAll(t, st, in)
	require.Len(t, out, 12-4+transcode.MaxFill)
	for i, b := range out[2 : 2+transcode.MaxFill] {
		assert.True(t, b.fill, "fill %d", i)
		assert.Equal(t, uint(6-transcode.MaxFill+i), b.pos, "fill %d", i)
		assert.Equal(t, uint(1+6-transcode.MaxFill+i), b.seq, "fill %d", i)
	}
	assert.Equal(t, transcode.MaxFill*layer2.VoiceSlotsPerBurst, srv.LostFrames())
}

// TestALateBurstAfterItsFillHasNoCopy delivers a lost burst after the
// fill took its place. The copy has the fill already, so the burst gets
// no copy and the chip does not decode it out of order.
func TestALateBurstAfterItsFillHasNoCopy(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	slots := ambeFrames(6 * layer2.VoiceSlotsPerBurst)
	full := seqCall(t, voiceLC(t, enums.FIDStandardized), slots)
	in := append(without(full[:1+4], 1+2), full[1+2])
	in = append(in, full[1+4:]...)

	st, err := svc.NewStream(transcode.CodecAMBE, transcode.CodecCodec2, codec2.Mode2400)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	for i := range in {
		r, ok := st.Convert(t.Context(), &in[i], deadline(st))
		if in[i].Seq == full[1+2].Seq && i == 4 {
			assert.False(t, ok, "the late burst has a copy")
			continue
		}
		require.True(t, ok, "packet %d", i)
		require.Zero(t, r.Missed)
	}
	assert.Equal(t, len(slots)-3, srv.Decodes())
	assert.Equal(t, 3, srv.LostFrames())
}

// TestBurstsWithoutSequenceNumbersAreNotFilled converts a call whose
// packets all carry sequence number 0, as some test clients send them.
// Nothing tells a lost burst, so the copy gets no fill.
func TestBurstsWithoutSequenceNumbersAreNotFilled(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	in := call(t, voiceLC(t, enums.FIDStandardized), ambeFrames(6*layer2.VoiceSlotsPerBurst))
	st, err := svc.NewStream(transcode.CodecAMBE, transcode.CodecCodec2, codec2.Mode2400)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	out, _ := convertAll(t, st, in)
	assert.Len(t, out, 6)
	assert.Zero(t, srv.LostFrames())
}

// TestCloseClearsLostFrame ends a call whose last request to the chip was
// for a lost frame: the deadline leaves no time to decode the burst after
// the fill. Close clears LOST_FRAME with a PKT_DCMODE field, and decodes
// nothing, so that the chip does not keep it set for the next call on it.
// The budget runs on a modelClock, which each answer moves by 60 ms.
func TestCloseClearsLostFrame(t *testing.T) {
	t.Parallel()
	const delay = 60 * time.Millisecond
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	svc.SetTiming(patientAttempt+5*delay/2, patientAttempt)
	clock := newModelClock(0)
	transcode.SetClock(svc, clock)
	slots := ambeFrames(6 * layer2.VoiceSlotsPerBurst)
	in := without(seqCall(t, voiceLC(t, enums.FIDStandardized), slots), 1+1)

	st, err := svc.NewStream(transcode.CodecAMBE, transcode.CodecCodec2, codec2.Mode2400)
	require.NoError(t, err)
	for i := range in[:2] {
		_, ok := st.Convert(t.Context(), &in[i], deadline(st))
		require.True(t, ok)
	}
	srv.SetReplyHook(func() { clock.advance(delay) })
	r, ok := st.Convert(t.Context(), &in[2], deadline(st))
	require.True(t, ok)
	require.Len(t, r.Fill, 1)
	require.Equal(t, layer2.VoiceSlotsPerBurst, r.Missed, "the burst after the fill has no time to decode")
	require.Equal(t, 3, srv.LostFrames())
	require.Equal(t, 1, srv.DCModes())
	decodes := srv.Decodes()
	st.Close()
	srv.SetReplyHook(nil)
	assert.Equal(t, 2, srv.DCModes(), "Close did not send PKT_DCMODE")
	assert.Equal(t, decodes, srv.Decodes(), "Close decoded a frame")

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "udp", srv.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	c, err := dvsi.New(conn, &dvsi.Config{Timeout: time.Second, NoReset: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	pcm, rep, err := c.Decode(nil, slots[9])
	require.NoError(t, err)
	assert.Equal(t, vocoder.FrameReport{}, rep, "the chip still has LOST_FRAME set")
	want := testutils.FakeAMBEDecode(slots[9])
	assert.Equal(t, want[:], pcm)
	assert.Empty(t, srv.Errors())
}

func TestFrameLabels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		r             vocoder.FrameReport
		frame, output string
	}{
		{vocoder.FrameReport{}, "good", "voice"},
		{vocoder.FrameReport{Frame: vocoder.FrameLost, Output: vocoder.OutputRepeat}, "lost", "repeat"},
		{vocoder.FrameReport{Frame: vocoder.FrameErased, Output: vocoder.OutputSilence}, "erased", "silence"},
		{vocoder.FrameReport{Frame: vocoder.FrameInvalid, Output: vocoder.OutputTone}, "invalid", "tone"},
		{vocoder.FrameReport{Frame: 9, Output: 9}, "unknown", "unknown"},
	} {
		frame, output := transcode.FrameLabels(tc.r)
		assert.Equal(t, tc.frame, frame)
		assert.Equal(t, tc.output, output)
	}
}
