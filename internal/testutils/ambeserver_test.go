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

package testutils_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/dvsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeSlot(i byte) [9]byte {
	var f vocoder.AMBEFrame
	for j := range 6 {
		f[j] = i*41 + byte(j)*17 + 3
	}
	s, _ := vocoder.EncodeAMBE72(f)
	return s
}

func dialFake(t *testing.T, srv *testutils.FakeAMBEServer) (net.Conn, *dvsi.Codec) {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "udp", srv.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	c, err := dvsi.New(conn, &dvsi.Config{Timeout: time.Second, NoReset: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return conn, c
}

func rawPacket(typ dvsi.PacketType, fields ...byte) []byte {
	n := len(fields) + 2
	b := append([]byte{dvsi.StartByte, byte(n >> 8), byte(n), byte(typ)}, fields...)
	b = append(b, 0x2F)
	var x byte
	for _, c := range b[1:] {
		x ^= c
	}
	return append(b, x)
}

func TestFakeAMBEServerReportsTheDecoderStatus(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	_, c := dialFake(t, srv)
	require.True(t, srv.SpeechFormat())

	a, b, tone := fakeSlot(1), fakeSlot(2), fakeSlot(3)
	srv.SetDecodeStatus(b, testutils.AMBEVoiceActive|testutils.AMBEDataInvalid)
	srv.SetDecodeStatus(tone, testutils.AMBEVoiceActive|testutils.AMBEToneFrame)
	for _, tc := range []struct {
		slot   [9]byte
		frame  vocoder.FrameStatus
		output vocoder.FrameOutput
	}{
		{a, vocoder.FrameGood, vocoder.OutputVoice},
		{b, vocoder.FrameErased, vocoder.OutputRepeat},
		{tone, vocoder.FrameGood, vocoder.OutputTone},
	} {
		pcm, rep, err := c.Decode(nil, tc.slot)
		require.NoError(t, err)
		want := testutils.FakeAMBEDecode(tc.slot)
		assert.Equal(t, want[:], pcm)
		assert.Equal(t, tc.frame, rep.Frame)
		assert.Equal(t, tc.output, rep.Output)
		assert.False(t, rep.CountsKnown)
	}
	assert.Empty(t, srv.Errors())
}

func TestFakeAMBEServerRepeatsLostFrames(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	_, c := dialFake(t, srv)
	a, b := fakeSlot(4), fakeSlot(5)
	wantA, wantB := testutils.FakeAMBEDecode(a), testutils.FakeAMBEDecode(b)

	_, _, err := c.Decode(nil, a)
	require.NoError(t, err)
	for i, want := range []struct {
		pcm    []int16
		output vocoder.FrameOutput
	}{
		{wantA[:], vocoder.OutputRepeat},
		{wantA[:], vocoder.OutputRepeat},
		{make([]int16, dvsi.FrameSamples), vocoder.OutputSilence},
		{make([]int16, dvsi.FrameSamples), vocoder.OutputSilence},
	} {
		pcm, rep, err := c.DecodeLost(nil)
		require.NoError(t, err, "lost frame %d", i)
		assert.Equal(t, want.pcm, pcm, "lost frame %d", i)
		assert.Equal(t, vocoder.FrameLost, rep.Frame, "lost frame %d", i)
		assert.Equal(t, want.output, rep.Output, "lost frame %d", i)
	}
	pcm, rep, err := c.Decode(nil, b)
	require.NoError(t, err)
	assert.Equal(t, wantB[:], pcm, "CMODE 0x0000 clears LOST_FRAME")
	assert.Equal(t, vocoder.FrameReport{}, rep)
	assert.Equal(t, 4, srv.LostFrames())
	assert.Equal(t, 5, srv.CModes())
	assert.Equal(t, 2, srv.Decodes())
	assert.Empty(t, srv.Errors())
}

func TestFakeAMBEServerKeepsLostFrameUntilCleared(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	conn, c := dialFake(t, srv)
	assert.Equal(t, 1, srv.DCModes(), "New clears DCMODE_IN")
	a, b := fakeSlot(7), fakeSlot(8)
	wantB := testutils.FakeAMBEDecode(b)

	_, _, err := c.Decode(nil, a)
	require.NoError(t, err)
	_, _, err = c.DecodeLost(nil)
	require.NoError(t, err)
	require.NoError(t, c.Close())
	assert.Equal(t, 2, srv.DCModes(), "Close after a lost frame clears DCMODE_IN")
	_, d := dialFake(t, srv)
	pcm, rep, err := d.Decode(nil, b)
	require.NoError(t, err)
	assert.Equal(t, wantB[:], pcm)
	assert.Equal(t, vocoder.FrameReport{}, rep)

	_, err = conn.Write(rawPacket(dvsi.TypeChannel, 0x02, 0x00, 0x04))
	require.NoError(t, err)
	buf := make([]byte, dvsi.MaxPacketLen)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = conn.Read(buf)
	require.NoError(t, err)
	_, err = conn.Write(rawPacket(dvsi.TypeChannel, append([]byte{0x01, dvsi.FrameBits}, b[:]...)...))
	require.NoError(t, err)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	p, _, err := dvsi.Parse(buf[:n], true)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x02, 0x00, byte(testutils.AMBEVoiceActive | testutils.AMBEDataInvalid)}, p.Fields[len(p.Fields)-3:], "LOST_FRAME stays set for every client")

	_, e := dialFake(t, srv)
	assert.Equal(t, 4, srv.DCModes())
	pcm, rep, err = e.Decode(nil, b)
	require.NoError(t, err)
	assert.Equal(t, wantB[:], pcm, "PKT_DCMODE 0x0000 clears LOST_FRAME")
	assert.Equal(t, vocoder.FrameReport{}, rep)
	assert.Equal(t, 3, srv.LostFrames())
	assert.Empty(t, srv.Errors())
}

func TestFakeAMBEServerRefusesTheSpeechFormat(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	srv.SetRefuseSpeechFormat(true)
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "udp", srv.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = dvsi.New(conn, &dvsi.Config{Timeout: time.Second, NoReset: true})
	require.ErrorIs(t, err, dvsi.ErrSpeechFormat)
	assert.False(t, srv.SpeechFormat())
}

func TestFakeAMBEServerDropsMisuse(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	conn, _ := dialFake(t, srv)
	slot := fakeSlot(6)
	for _, tc := range []struct {
		name   string
		typ    dvsi.PacketType
		fields []byte
		err    string
	}{
		{"other CMODE flags", dvsi.TypeChannel, append([]byte{0x02, 0x00, 0x10, 0x01, dvsi.FrameBits}, slot[:]...), "CMODE 0x0010"},
		{"no CHAND without LOST_FRAME", dvsi.TypeChannel, []byte{0x02, 0x00, 0x00}, "no CHAND"},
		{"nothing", dvsi.TypeChannel, nil, "no CHAND"},
		{"other PKT_DCMODE flags", dvsi.TypeControl, []byte{0x40, 0x06, 0x00, 0x10}, "PKT_DCMODE 0x0010"},
		{"short PKT_DCMODE", dvsi.TypeControl, []byte{0x40, 0x06, 0x00}, "short PKT_DCMODE"},
	} {
		before := len(srv.Errors())
		_, err := conn.Write(rawPacket(tc.typ, tc.fields...))
		require.NoError(t, err, tc.name)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(150*time.Millisecond)))
		buf := make([]byte, dvsi.MaxPacketLen)
		_, err = conn.Read(buf)
		require.Error(t, err, "%s: the server answered", tc.name)
		errs := srv.Errors()
		require.Len(t, errs, before+1, tc.name)
		assert.True(t, strings.Contains(errs[before], tc.err), "%s: %q", tc.name, errs[before])
	}
}
