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
	"context"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func speech(n int) [][]int16 {
	out := make([][]int16, n)
	for i := range out {
		out[i] = make([]int16, codec2.FrameSamples)
		f := 180 + 40*float64(i%7)
		for j := range out[i] {
			t := float64(i*codec2.FrameSamples+j) / 8000
			out[i][j] = int16(6000*math.Sin(2*math.Pi*f*t) + 2500*math.Sin(2*math.Pi*3*f*t))
		}
	}
	return out
}

func codec2Frames(t *testing.T, mode codec2.Mode, n int) [][9]byte {
	t.Helper()
	enc, err := codec2.New(&codec2.Config{Mode: mode})
	require.NoError(t, err)
	t.Cleanup(func() { _ = enc.Close() })
	out := make([][9]byte, 0, n)
	for _, pcm := range speech(n) {
		s, err := enc.Encode(pcm)
		require.NoError(t, err)
		out = append(out, s)
	}
	return out
}

func ambeFrames(n int) [][9]byte {
	out := make([][9]byte, n)
	for i := range out {
		var f vocoder.AMBEFrame
		for j := range 6 {
			f[j] = byte(i*29 + j*83 + 7)
		}
		f[6] = byte(i<<7) & 0x80
		out[i], _ = vocoder.EncodeAMBE72(f)
	}
	return out
}

func voiceLC(t *testing.T, fid enums.FID) pdu.LCBytes {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(true, 91, 1000001, layer3.ServiceOptions{IsOpenVoiceCallMode: true, PriorityLevel: 2})
	require.NoError(t, err)
	lc.SetFID(fid)
	return lc
}

// call returns the packets of a call with the LC lc and the voice slots:
// the Voice LC Header, the voice bursts and the Terminator with LC.
func call(t *testing.T, lc pdu.LCBytes, slots [][9]byte) []models.Packet {
	t.Helper()
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: layer2.SyncSourceMS})
	require.NoError(t, err)
	pkt := func(raw layer2.RawBurst, ft dmrconst.FrameType, v uint) models.Packet {
		return models.Packet{Signature: string(dmrconst.CommandDMRD), Src: 1000001, Dst: 91, GroupCall: true, StreamID: 77, FrameType: ft, DTypeOrVSeq: v, DMRData: raw}
	}
	out := []models.Packet{pkt(vt.Header(), dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader))}
	bursts, err := vt.AddSlots(slots...)
	require.NoError(t, err)
	for i, raw := range bursts {
		ft := dmrconst.FrameVoice
		if i%6 == 0 {
			ft = dmrconst.FrameVoiceSync
		}
		out = append(out, pkt(raw, ft, uint(i%6)))
	}
	return append(out, pkt(vt.Terminator(), dmrconst.FrameDataSync, uint(enums.DataTypeTerminatorWithLC)))
}

// patientAttempt is how long a request of a stream of newServers waits
// for its answer. Only a request that the server does not answer waits it
// out; a test that loses answers uses modeledService or newRealService.
const patientAttempt = 30 * time.Second

func newService(t *testing.T, addr string) *transcode.Service {
	t.Helper()
	return newServers(t, nil, transcode.ServerConfig{Addr: addr})
}

// newServers returns a service for list whose streams measure the budget
// of a burst on a clock that does not move and wait patientAttempt for
// each answer, so that what a test converts does not depend on how loaded
// the machine is.
func newServers(t *testing.T, store kv.KV, list ...transcode.ServerConfig) *transcode.Service {
	t.Helper()
	s := newRealServers(t, store, list...)
	s.SetTiming(3*patientAttempt, patientAttempt)
	transcode.SetClock(s, clock.NewFake(time.Now()))
	return s
}

// newRealService returns a service for addr with the timing of the hub,
// on the wall clock.
func newRealService(t *testing.T, addr string) *transcode.Service {
	t.Helper()
	return newRealServers(t, nil, transcode.ServerConfig{Addr: addr})
}

func newRealServers(t *testing.T, store kv.KV, list ...transcode.ServerConfig) *transcode.Service {
	t.Helper()
	s := transcode.NewService(store, clock.Real())
	t.Cleanup(s.Close)
	s.SetServers(list)
	return s
}

// deadline returns the end of the budget of a burst that st gets now.
func deadline(st *transcode.Stream) time.Time {
	return transcode.StreamNow(st).Add(st.Budget())
}

func answered(svc *transcode.Service, i int) bool {
	st := svc.Status(context.Background())
	return i < len(st) && st[i].Answered
}

func convert(t *testing.T, st *transcode.Stream, in []models.Packet) ([]layer2.RawBurst, int) {
	t.Helper()
	out := make([]layer2.RawBurst, 0, len(in))
	missed := 0
	for i := range in {
		r, ok := st.Convert(t.Context(), &in[i], deadline(st))
		require.True(t, ok, "burst %d has no copy", i)
		out = append(out, r.Burst)
		missed += r.Missed
	}
	return out, missed
}

func requireLC(t *testing.T, raw layer2.RawBurst, dt enums.DataType, want pdu.LCBytes) {
	t.Helper()
	p, ok := raw.MMDVMHostPayload(dt)
	require.True(t, ok, "%s: the LC does not pass its check", dt)
	require.Equal(t, want, pdu.LCBytes(p[:9]), "%s LC", dt)
}

// checkCopy checks the LC of the copy out of the call in: the FID of the
// copy in the Voice LC Header, the Terminator with LC and the embedded LC
// of each superframe, and the EMB and sync of each voice burst unchanged.
func checkCopy(t *testing.T, in []models.Packet, out []layer2.RawBurst, want pdu.LCBytes) {
	t.Helper()
	requireLC(t, out[0], enums.DataTypeVoiceLCHeader, want)
	requireLC(t, out[len(out)-1], enums.DataTypeTerminatorWithLC, want)
	for i := 1; i < len(out)-1; i++ {
		raw, orig := out[i], layer2.RawBurst(in[i].DMRData)
		if in[i].DTypeOrVSeq == 0 {
			s1, _ := raw.Sync()
			s2, _ := orig.Sync()
			require.Equal(t, s2, s1, "burst %d sync", i)
		} else {
			require.Equal(t, orig.EMB(), raw.EMB(), "burst %d EMB", i)
		}
		if in[i].DTypeOrVSeq == 1 && i+3 < len(out) {
			var f [4][4]byte
			for k := range 4 {
				f[k] = out[i+k].EmbeddedData()
			}
			lc, res := layer2.DecodeEmbeddedLC(f)
			require.False(t, res.Uncorrectable, "embedded LC at burst %d", i)
			require.Equal(t, want, pdu.LCBytes(lc), "embedded LC at burst %d", i)
		}
	}
}

func TestHeaderCodec(t *testing.T) {
	t.Parallel()
	c2 := call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, 3))
	codec, lc, ok := transcode.HeaderCodec(&c2[0])
	require.True(t, ok)
	assert.Equal(t, transcode.CodecCodec2, codec)
	assert.Equal(t, voiceLC(t, transcode.Codec2FID), lc)

	ambe := call(t, voiceLC(t, enums.FIDStandardized), ambeFrames(3))
	codec, _, ok = transcode.HeaderCodec(&ambe[0])
	require.True(t, ok)
	assert.Equal(t, transcode.CodecAMBE, codec)

	moto := call(t, voiceLC(t, 0x10), ambeFrames(3))
	codec, _, ok = transcode.HeaderCodec(&moto[0])
	require.True(t, ok)
	assert.Equal(t, transcode.CodecAMBE, codec, "only the FID 0xC2 is Codec2")

	_, _, ok = transcode.HeaderCodec(&c2[1])
	assert.False(t, ok, "a voice burst is not a header")
	_, _, ok = transcode.HeaderCodec(&c2[len(c2)-1])
	assert.False(t, ok, "a terminator does not start a call")

	bad := c2[0]
	p, _ := layer2.RawBurst(bad.DMRData).BPTCPayload()
	p[1] ^= 0x01
	raw := layer2.RawBurst(bad.DMRData)
	raw.SetBPTCPayload(p)
	bad.DMRData = raw
	_, _, ok = transcode.HeaderCodec(&bad)
	assert.False(t, ok, "a header whose LC fails its check gives no codec")
}

func TestEmbeddedCodec(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fid  enums.FID
		want transcode.Codec
	}{{transcode.Codec2FID, transcode.CodecCodec2}, {enums.FIDStandardized, transcode.CodecAMBE}, {0x10, transcode.CodecAMBE}} {
		pkts := call(t, voiceLC(t, tc.fid), ambeFrames(18))
		var e transcode.EmbeddedCodec
		for i, p := range pkts[:5] {
			_, _, ok := e.Add(&p)
			require.False(t, ok, "FID %#x: packet %d decides before burst E", tc.fid, i)
		}
		codec, lc, ok := e.Add(&pkts[5])
		require.True(t, ok, "FID %#x: burst E completes the LC", tc.fid)
		assert.Equal(t, tc.want, codec, "FID %#x", tc.fid)
		assert.Equal(t, voiceLC(t, tc.fid), lc)
	}

	pkts := call(t, voiceLC(t, transcode.Codec2FID), ambeFrames(18))
	raw := layer2.RawBurst(pkts[3].DMRData)
	d := raw.EmbeddedData()
	d[0] ^= 0xFF
	d[2] ^= 0xFF
	raw.SetEmbeddedData(d)
	pkts[3].DMRData = raw
	var e transcode.EmbeddedCodec
	for i := range pkts[:7] {
		_, _, ok := e.Add(&pkts[i])
		require.False(t, ok, "packet %d: an LC that fails its check gives no codec", i)
	}
}

func TestModeDetector(t *testing.T) {
	t.Parallel()
	for _, mode := range []codec2.Mode{codec2.Mode2400, codec2.Mode3200} {
		var d transcode.ModeDetector
		frames := codec2Frames(t, mode, 12)
		for i, f := range frames {
			d.Add(f)
			if d.Mode() != codec2.ModeAuto {
				assert.GreaterOrEqual(t, i, 4, "%d: decided after %d frames", mode, i+1)
				break
			}
		}
		assert.Equal(t, mode, d.Mode())
	}
	var quiet transcode.ModeDetector
	for range 20 {
		quiet.Add([9]byte{})
	}
	assert.Equal(t, codec2.ModeAuto, quiet.Mode(), "silence decides nothing")
}

func TestCodec2ToAMBE(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	slots := codec2Frames(t, codec2.Mode3200, 12*layer2.VoiceSlotsPerBurst)
	in := call(t, voiceLC(t, transcode.Codec2FID), slots)

	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	out, missed := convert(t, st, in)
	require.Zero(t, missed)

	pcm := srv.EncodedPCM()
	require.Len(t, pcm, len(slots))
	for i, raw := range out[1 : len(out)-1] {
		got := raw.VoiceSlots()
		for k := range got {
			require.Equal(t, testutils.FakeAMBEEncode(pcm[i*3+k][:]), got[k], "burst %d frame %d", i+1, k)
		}
	}
	checkDecoded(t, slots, pcm)
	checkCopy(t, in, out, voiceLC(t, enums.FIDStandardized))
	assert.Equal(t, len(slots), srv.Encodes())
	assert.Empty(t, srv.Errors())
	assert.True(t, answered(svc, 0))
}

func TestAMBEToCodec2(t *testing.T) {
	t.Parallel()
	for _, mode := range []codec2.Mode{codec2.ModeAuto, codec2.Mode2400, codec2.Mode3200} {
		srv := testutils.StartFakeAMBEServer(t)
		svc := newService(t, srv.Addr())
		slots := ambeFrames(12 * layer2.VoiceSlotsPerBurst)
		in := call(t, voiceLC(t, enums.FIDStandardized), slots)

		st, err := svc.NewStream(transcode.CodecAMBE, transcode.CodecCodec2, mode)
		require.NoError(t, err)
		t.Cleanup(st.Close)
		out, missed := convert(t, st, in)
		require.Zero(t, missed)

		want := mode
		if want == codec2.ModeAuto {
			want = codec2.Mode2400
		}
		enc, err := codec2.New(&codec2.Config{Mode: want})
		require.NoError(t, err)
		t.Cleanup(func() { _ = enc.Close() })
		var det transcode.ModeDetector
		for i, raw := range out[1 : len(out)-1] {
			got := raw.VoiceSlots()
			for k := range got {
				pcm := testutils.FakeAMBEDecode(slots[i*3+k])
				exp, err := enc.Encode(pcm[:])
				require.NoError(t, err)
				require.Equal(t, exp, got[k], "mode %d burst %d frame %d", mode, i+1, k)
				det.Add(got[k])
			}
		}
		assert.Equal(t, want, det.Mode())
		checkCopy(t, in, out, voiceLC(t, transcode.Codec2FID))
		assert.Equal(t, len(slots), srv.Decodes())
		assert.Empty(t, srv.Errors())
	}
}

// TestLateEntryLearnsTheLC converts a call that starts without its Voice
// LC Header. The copy keeps the embedded LC of the first superframe, which
// teaches the stream the LC, and has the LC of the copy from the second on.
func TestLateEntryLearnsTheLC(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	in := call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, 18*layer2.VoiceSlotsPerBurst))
	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	voice := in[1 : len(in)-1]
	out, missed := convert(t, st, voice)
	require.Zero(t, missed)
	for k := 1; k <= 4; k++ {
		assert.Equal(t, layer2.RawBurst(voice[k].DMRData).EmbeddedData(), out[k].EmbeddedData(), "first superframe, burst %d", k)
	}
	for sf := 1; sf < 3; sf++ {
		var f [4][4]byte
		for k := range 4 {
			f[k] = out[sf*6+1+k].EmbeddedData()
		}
		lc, res := layer2.DecodeEmbeddedLC(f)
		require.False(t, res.Uncorrectable)
		assert.Equal(t, voiceLC(t, enums.FIDStandardized), pdu.LCBytes(lc), "superframe %d", sf)
	}
}

// TestRetryWithinTheBudget loses the reply to one frame. The frame is
// asked again within the budget of its burst, so no audio is missed.
func TestRetryWithinTheBudget(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc, clock := modeledService(t, srv)
	in := call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, 6*layer2.VoiceSlotsPerBurst))
	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	for i := range in {
		if i == 3 {
			srv.DropNext(1)
		}
		r, ok := st.Convert(t.Context(), &in[i], clock.now().Add(st.Budget()))
		require.True(t, ok)
		assert.Zero(t, r.Missed, "packet %d: the lost frame was not asked again", i)
	}
	assert.Equal(t, 1, clock.drops(), "the server did not drop a reply")
	assert.Empty(t, srv.Errors())
}

// TestServerDownMissesWithoutWaiting stops the AMBE server in the middle
// of a call. Each burst still comes back within its budget, with silence
// in place of the frames, and conversion resumes once the server answers.
// The budget runs on a modelClock, which each unanswered request moves by
// an attempt.
func TestServerDownMissesWithoutWaiting(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc, clock := modeledService(t, srv)
	slots := codec2Frames(t, codec2.Mode2400, 18*layer2.VoiceSlotsPerBurst)
	in := call(t, voiceLC(t, transcode.Codec2FID), slots)
	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)

	_, missed := convert(t, st, in[:7])
	require.Zero(t, missed)

	srv.SetAnswering(false)
	for i := 7; i < 13; i++ {
		start := clock.now()
		r, ok := st.Convert(t.Context(), &in[i], start.Add(st.Budget()))
		took := clock.now().Sub(start)
		require.True(t, ok)
		assert.Equal(t, 3, r.Missed, "burst %d", i)
		assert.Equal(t, transcode.ReasonTimeout, r.Reason, "burst %d", i)
		assert.LessOrEqual(t, took, st.Budget(), "burst %d waited for the server", i)
		for k, s := range r.Burst.VoiceSlots() {
			assert.Equal(t, vocoder.AMBESilenceSlot(), s, "burst %d frame %d", i, k)
		}
	}

	srv.SetAnswering(true)
	out, missed := convert(t, st, in[13:])
	assert.Zero(t, missed, "conversion did not resume")
	pcm := srv.EncodedPCM()
	require.Len(t, pcm, 6*3+len(slots)-12*3)
	assert.Equal(t, testutils.FakeAMBEEncode(pcm[6*3][:]), out[0].VoiceSlots()[0])
	if codec2.Backend == "wasm" {
		dec, err := codec2.New(nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = dec.Close() })
		var want []int16
		for _, s := range slots[:12*3+1] {
			want, _, err = dec.Decode(want[:0], s)
			require.NoError(t, err)
		}
		assert.Equal(t, want, pcm[6*3][:], "the decoder lost its place while the server was down")
	}
}

// checkDecoded checks that pcm is the Codec2 decoding of slots. Only the
// WebAssembly backend decodes the same samples every time: the native
// libcodec2 shares one random number generator between its decoders. The
// native backend is checked for audio that is not silence.
func checkDecoded(t *testing.T, slots [][9]byte, pcm [][160]int16) {
	t.Helper()
	require.Len(t, pcm, len(slots))
	if codec2.Backend != "wasm" {
		for i := range pcm {
			var e float64
			for _, v := range pcm[i] {
				e += float64(v) * float64(v)
			}
			require.Positive(t, e, "frame %d is silence", i)
		}
		return
	}
	dec, err := codec2.New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dec.Close() })
	for i, s := range slots {
		want, _, err := dec.Decode(nil, s)
		require.NoError(t, err)
		require.Equal(t, want, pcm[i][:], "frame %d", i)
	}
}

// TestUnreachableServerFailsFast points the service at a port where nothing
// listens. Bursts come back at once with silence.
func TestUnreachableServerFailsFast(t *testing.T) {
	t.Parallel()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	addr := conn.LocalAddr().String()
	require.NoError(t, conn.Close())
	svc := newRealService(t, addr)
	in := call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, 6*layer2.VoiceSlotsPerBurst))
	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	start := time.Now()
	_, missed := convert(t, st, in)
	assert.Equal(t, 6*3, missed)
	assert.Less(t, time.Since(start), time.Duration(len(in))*transcode.Budget)
	assert.False(t, answered(svc, 0))
}

func TestServiceOff(t *testing.T) {
	t.Parallel()
	svc := transcode.NewService(nil, clock.Real())
	t.Cleanup(svc.Close)
	assert.False(t, svc.Enabled())
	_, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.ErrorIs(t, err, transcode.ErrOff)

	srv := testutils.StartFakeAMBEServer(t)
	svc.SetServers([]transcode.ServerConfig{{Addr: srv.Addr(), Calls: 2}})
	st := svc.Status(context.Background())
	require.Len(t, st, 1)
	assert.Equal(t, transcode.ServerConfig{Addr: srv.Addr(), Calls: 2}, st[0].ServerConfig)
	_, err = svc.NewStream(transcode.CodecAMBE, transcode.CodecAMBE, codec2.ModeAuto)
	require.ErrorIs(t, err, transcode.ErrCodecs)
	svc.SetServers(nil)
	assert.False(t, svc.Enabled())
}

func TestProbeShowsTheServerAnswers(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	require.Eventually(t, func() bool { return answered(svc, 0) }, 5*time.Second, 10*time.Millisecond)
	assert.Positive(t, srv.Queries())
	assert.Empty(t, srv.Errors())

	quiet := testutils.StartFakeAMBEServer(t)
	quiet.SetAnswering(false)
	svc.SetServers([]transcode.ServerConfig{{Addr: srv.Addr()}, {Addr: quiet.Addr()}})
	assert.True(t, answered(svc, 0), "a server that stays keeps its state")
	assert.False(t, answered(svc, 1), "the answer of another server shows for the new one")
}

// TestProbesComeFromOneSocket probes a server twice; both queries must
// come from the same address, so the server sees one client and not a new
// one every ProbeInterval.
func TestProbesComeFromOneSocket(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	srv.SetControlParity(false)
	newService(t, srv.Addr())
	require.Eventually(t, func() bool { return srv.Queries() >= 2 }, 3*transcode.ProbeInterval, 50*time.Millisecond)
	assert.Equal(t, 1, srv.QuerySources())
}

// TestProbeTakesAReplyWithoutParity probes a server whose chip has parity
// mode off, so its replies carry no parity field.
func TestProbeTakesAReplyWithoutParity(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	srv.SetControlParity(false)
	svc := newService(t, srv.Addr())
	require.Eventually(t, func() bool { return answered(svc, 0) }, 5*time.Second, 10*time.Millisecond)
	assert.Empty(t, srv.Errors())
}

// TestEmbeddedLCThatDiffersFromTheHeader converts a call whose embedded LC
// has other Service Options than its Voice LC Header. The copy must carry
// the embedded LC with the FID of the copy from the second superframe on.
func TestEmbeddedLCThatDiffersFromTheHeader(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newService(t, srv.Addr())
	in := call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, 18*layer2.VoiceSlotsPerBurst))
	other, err := pdu.NewVoiceLCBytes(true, 91, 1000001, layer3.ServiceOptions{IsEmergency: true})
	require.NoError(t, err)
	other.SetFID(transcode.Codec2FID)
	embedded := call(t, other, codec2Frames(t, codec2.Mode2400, 18*layer2.VoiceSlotsPerBurst))
	for i := 1; i < len(in)-1; i++ {
		raw := layer2.RawBurst(in[i].DMRData)
		raw.SetEmbeddedData(layer2.RawBurst(embedded[i].DMRData).EmbeddedData())
		in[i].DMRData = raw
	}
	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	out, missed := convert(t, st, in)
	require.Zero(t, missed)
	want := other
	want.SetFID(0)
	for sf := 1; sf < 3; sf++ {
		var f [4][4]byte
		for k := range 4 {
			f[k] = out[1+sf*6+1+k].EmbeddedData()
		}
		lc, res := layer2.DecodeEmbeddedLC(f)
		require.False(t, res.Uncorrectable)
		assert.Equal(t, want, pdu.LCBytes(lc), "superframe %d", sf)
	}
}

// TestALateBurstDoesNotCountAgainstTheServer gives a burst no time left in
// its budget. Its frames are late, and the next burst still gets the retry
// that a lost answer needs.
func TestALateBurstDoesNotCountAgainstTheServer(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc, clock := modeledService(t, srv)
	in := call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, 6*layer2.VoiceSlotsPerBurst))
	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	for i := range in[:2] {
		r, ok := st.Convert(t.Context(), &in[i], clock.now().Add(st.Budget()))
		require.True(t, ok)
		require.Zero(t, r.Missed)
	}
	r, ok := st.Convert(t.Context(), &in[2], clock.now().Add(time.Millisecond))
	require.True(t, ok)
	assert.Equal(t, 3, r.Missed)
	assert.Equal(t, transcode.ReasonLate, r.Reason)
	srv.DropNext(1)
	r, ok = st.Convert(t.Context(), &in[3], clock.now().Add(st.Budget()))
	require.True(t, ok)
	assert.Zero(t, r.Missed, "a late burst made the stream give up the retry")
	assert.Equal(t, 1, clock.drops(), "the server did not drop a reply")
}

// modelClock is a clock for the budget of a stream that moves only by the
// waits it models: each reply that the fake AMBE server drops costs one
// attempt, the time the stream waits for it. The other work of a burst
// takes no time on it, so that the decisions of a stream do not depend on
// how loaded the machine is.
type modelClock struct {
	*clock.Fake
	attempt time.Duration
	n       atomic.Int64
}

func newModelClock(attempt time.Duration) *modelClock {
	return &modelClock{Fake: clock.NewFake(time.Now()), attempt: attempt}
}

func (c *modelClock) now() time.Time {
	return c.Now()
}

func (c *modelClock) dropped() {
	c.Advance(c.attempt)
	c.n.Add(1)
}

func (c *modelClock) advance(d time.Duration) {
	c.Advance(d)
}

func (c *modelClock) drops() int {
	return int(c.n.Load())
}

// modeledService returns a service for srv whose streams run on a
// modelClock. The budget and the attempt keep the ratio of Budget to
// AttemptTimeout, scaled so that a real answer of the fake server comes
// within an attempt on a loaded machine; only a dropped reply waits it out.
func modeledService(t *testing.T, srv *testutils.FakeAMBEServer) (*transcode.Service, *modelClock) {
	t.Helper()
	const scale = 25
	svc := newService(t, srv.Addr())
	svc.SetTiming(scale*transcode.Budget, scale*transcode.AttemptTimeout)
	mc := newModelClock(scale * transcode.AttemptTimeout)
	transcode.SetClock(svc, mc)
	srv.SetDropHook(mc.dropped)
	return svc, mc
}

func memoryKV(t *testing.T) kv.KV {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

type overlap struct {
	in  []models.Packet
	st  *transcode.Stream
	out []layer2.RawBurst
	res []transcode.Result
}

// overlapping converts two Codec2 calls burst by burst at once, each in a
// goroutine of its own, as two streams of the hub do.
func overlapping(t *testing.T, svc *transcode.Service) [2]*overlap {
	t.Helper()
	var calls [2]*overlap
	for i := range calls {
		st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
		require.NoError(t, err)
		t.Cleanup(st.Close)
		calls[i] = &overlap{in: call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, 6*layer2.VoiceSlotsPerBurst)), st: st}
	}
	var wg sync.WaitGroup
	for _, c := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range c.in {
				r, ok := c.st.Convert(context.Background(), &c.in[i], deadline(c.st))
				if ok {
					c.out = append(c.out, r.Burst)
					c.res = append(c.res, r)
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
	return calls
}

func busyAndConverted(t *testing.T, calls [2]*overlap) (busy, converted *overlap) {
	t.Helper()
	for _, c := range calls {
		require.NotEmpty(t, c.res)
		if c.res[0].Busy {
			busy = c
		} else {
			converted = c
		}
	}
	return busy, converted
}

func requireConverted(t *testing.T, c *overlap) {
	t.Helper()
	missed := 0
	for _, r := range c.res {
		require.False(t, r.Busy)
		missed += r.Missed
	}
	require.Zero(t, missed, "the call lost frames")
	checkCopy(t, c.in, c.out, voiceLC(t, enums.FIDStandardized))
}

// TestALimitOneServerTakesOneCallAtOnce converts two calls at once on an
// AMBEserver with one chip, which answers only its last client. One call
// gets the server and is converted whole; the other finds it busy and is
// not converted at all, so their answers never cross.
func TestALimitOneServerTakesOneCallAtOnce(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	srv.SetLastClientOnly(true)
	svc := newServers(t, memoryKV(t), transcode.ServerConfig{Addr: srv.Addr(), Calls: 1})
	require.Eventually(t, func() bool { return answered(svc, 0) }, 5*time.Second, 10*time.Millisecond)
	busy, converted := busyAndConverted(t, overlapping(t, svc))
	require.NotNil(t, busy, "both calls used the server with one chip")
	require.NotNil(t, converted)
	for _, r := range busy.res {
		assert.True(t, r.Busy)
		assert.Equal(t, transcode.ReasonBusy, r.Reason)
	}
	requireConverted(t, converted)
	pcm := srv.EncodedPCM()
	require.Len(t, pcm, 6*layer2.VoiceSlotsPerBurst)
	for i, raw := range converted.out[1 : len(converted.out)-1] {
		for k, slot := range raw.VoiceSlots() {
			require.Equal(t, testutils.FakeAMBEEncode(pcm[i*3+k][:]), slot, "burst %d frame %d", i+1, k)
		}
	}
	converted.st.Close()
	assert.Zero(t, svc.Status(context.Background())[0].CallsNow, "the call gave the server back")
}

// TestAServerWithoutLimitTakesBothCalls converts two calls at once on a
// server without a limit; both are converted, each on a socket of its own.
func TestAServerWithoutLimitTakesBothCalls(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	svc := newServers(t, memoryKV(t), transcode.ServerConfig{Addr: srv.Addr()})
	calls := overlapping(t, svc)
	assert.Equal(t, 2, svc.Status(context.Background())[0].CallsNow)
	for _, c := range calls {
		requireConverted(t, c)
	}
	assert.Equal(t, 2*6*3, srv.Encodes())
}

// TestTwoServersOfOneCallEachTakeTwoCalls converts two calls at once on two
// AMBEservers with one chip each. Each call gets a server of its own.
func TestTwoServersOfOneCallEachTakeTwoCalls(t *testing.T) {
	t.Parallel()
	one, two := testutils.StartFakeAMBEServer(t), testutils.StartFakeAMBEServer(t)
	one.SetLastClientOnly(true)
	two.SetLastClientOnly(true)
	svc := newServers(t, memoryKV(t), transcode.ServerConfig{Addr: one.Addr(), Calls: 1}, transcode.ServerConfig{Addr: two.Addr(), Calls: 1})
	require.Eventually(t, func() bool { return answered(svc, 0) && answered(svc, 1) }, 5*time.Second, 10*time.Millisecond)
	for _, c := range overlapping(t, svc) {
		requireConverted(t, c)
	}
	assert.Equal(t, 6*3, one.Encodes())
	assert.Equal(t, 6*3, two.Encodes())
}

// TestServerSlotsAreSharedThroughKV gives two Services, as two replicas,
// one KV store and one server with one slot. The second finds it busy
// while the first holds it, and gets it once the first lets it go.
func TestServerSlotsAreSharedThroughKV(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	store := memoryKV(t)
	cfg := transcode.ServerConfig{Addr: srv.Addr(), Calls: 1}
	a, b := newServers(t, store, cfg), newServers(t, store, cfg)
	in := call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, layer2.VoiceSlotsPerBurst))
	sa, err := a.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	r, _ := sa.Convert(t.Context(), &in[1], deadline(sa))
	require.False(t, r.Busy)
	assert.Equal(t, 1, b.Status(t.Context())[0].CallsNow, "the other replica sees the call")
	sb, err := b.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(sb.Close)
	r, _ = sb.Convert(t.Context(), &in[1], deadline(sb))
	assert.True(t, r.Busy, "two replicas together went over the limit of the server")
	sa.Close()
	sc, err := b.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(sc.Close)
	r, _ = sc.Convert(t.Context(), &in[1], deadline(sc))
	assert.False(t, r.Busy, "the slot did not come free")
}

// TestSetTimingGivesLaterCallsRoom answers each request after 30 ms, more
// than an attempt waits by default, so a call loses its frames. A call that
// starts after SetTiming gives the answers room is converted whole.
func TestSetTimingGivesLaterCallsRoom(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	srv.SetDelay(30 * time.Millisecond)
	svc := newRealService(t, srv.Addr())
	in := call(t, voiceLC(t, transcode.Codec2FID), codec2Frames(t, codec2.Mode2400, 2*layer2.VoiceSlotsPerBurst))
	st, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	_, missed := convert(t, st, in)
	assert.Positive(t, missed, "answers after 30 ms fit in the default attempt")

	svc.SetTiming(2*time.Second, 500*time.Millisecond)
	roomy, err := svc.NewStream(transcode.CodecCodec2, transcode.CodecAMBE, codec2.ModeAuto)
	require.NoError(t, err)
	t.Cleanup(roomy.Close)
	assert.Equal(t, 2*time.Second, roomy.Budget())
	for i := range in {
		r, ok := roomy.Convert(t.Context(), &in[i], time.Now().Add(roomy.Budget()))
		require.True(t, ok)
		assert.Zero(t, r.Missed, "burst %d", i)
	}
}
