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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
)

const (
	benchRepeater = 311860
	benchTG       = 3100
	benchRadio    = 3191868
	benchStep     = 5 * time.Millisecond
)

// bench runs a radio on a clock of its own and answers its status
// requests as a modem with room for every burst.
type bench struct {
	t          *testing.T
	m          config.Modem
	r          *radio
	now        time.Duration
	sent       []mmdvm.Frame
	routed     []models.Packet
	wantStatus bool
	silent     bool
}

func newBench(t *testing.T, duplex bool, protocol uint8) *bench {
	t.Helper()
	return newBenchRSSI(t, duplex, protocol, rssiMap{})
}

func newBenchRSSI(t *testing.T, duplex bool, protocol uint8, rssi rssiMap) *bench {
	t.Helper()
	b := &bench{t: t, m: mmdvmhostModem(), now: time.Second}
	b.m.Duplex, b.m.ColorCode = duplex, 1
	r, err := newRadio(&b.m, benchRepeater, protocol, b, b.now, rssi)
	if err != nil {
		t.Fatal(err)
	}
	b.r = r
	return b
}

func (b *bench) write(f []byte) {
	fr, n, res := mmdvm.Next(f)
	if res != mmdvm.NextFrame || n != len(f) {
		b.t.Fatalf("the radio wrote a bad frame %x", f)
	}
	fr.Payload = append([]byte(nil), fr.Payload...)
	b.sent = append(b.sent, fr)
	if fr.Command == mmdvm.CommandGetStatus {
		b.wantStatus = true
	}
}

func (b *bench) route(p models.Packet) { b.routed = append(b.routed, p) }

func (b *bench) status() {
	var p []byte
	var err error
	if b.r.protocol == 1 {
		s := mmdvm.StatusV1{DMR: true, DMRSpace1: 10, DMRSpace2: 10}
		p, err = s.AppendBinary(nil)
	} else {
		s := mmdvm.StatusV2{DMRSpace1: 10, DMRSpace2: 10}
		p, err = s.AppendBinary(nil)
	}
	if err != nil {
		b.t.Fatal(err)
	}
	b.r.frame(b.now, mmdvm.Frame{Command: mmdvm.CommandGetStatus, Payload: p})
}

// run advances the clock by d in steps of benchStep.
func (b *bench) run(d time.Duration) {
	for end := b.now + d; b.now < end; {
		b.now += benchStep
		b.r.tick(b.now)
		if b.wantStatus && !b.silent {
			b.wantStatus = false
			b.status()
		}
	}
}

func (b *bench) rx(s enums.Slot, c mmdvm.Control, burst layer2.RawBurst) {
	d := mmdvm.DMRData{Slot: s, Control: c, Burst: burst}
	p, err := d.AppendBinary(nil)
	if err != nil {
		b.t.Fatal(err)
	}
	b.r.frame(b.now, mmdvm.Frame{Command: d.Command(), Payload: p})
}

func (b *bench) count(c mmdvm.Command) int {
	n := 0
	for _, f := range b.sent {
		if f.Command == c {
			n++
		}
	}
	return n
}

// control returns the frames that the radio sent, without the status
// requests.
func (b *bench) control() []mmdvm.Frame {
	var out []mmdvm.Frame
	for _, f := range b.sent {
		if f.Command != mmdvm.CommandGetStatus {
			out = append(out, f)
		}
	}
	return out
}

func (b *bench) bursts(c mmdvm.Command) []layer2.RawBurst {
	var out []layer2.RawBurst
	for _, f := range b.sent {
		if f.Command != c {
			continue
		}
		d, err := mmdvm.DecodeDMRData(f)
		if err != nil {
			b.t.Fatal(err)
		}
		out = append(out, d.Burst)
	}
	return out
}

type timedBurst struct {
	control mmdvm.Control
	frame   dmrconst.FrameType
	value   uint
	burst   layer2.RawBurst
}

// voiceCall returns the bursts of a group voice call to benchTG from
// benchRadio, with the control octet the firmware gives each and the DMRD
// frame type.
func voiceCall(t testing.TB, src layer2.SyncSource, superframes int) []timedBurst {
	t.Helper()
	var lcb pdu.LCBytes
	if err := lcb.SetFLCO(enums.FLCOGroupVoiceChannelUser); err != nil {
		t.Fatal(err)
	}
	if err := lcb.SetCall(true, benchTG, benchRadio); err != nil {
		t.Fatal(err)
	}
	tx, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lcb), ColorCode: 1, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	raw := tx.Headers()
	frames := make([]vocoder.AMBEFrame, superframes*6*layer2.VoiceSlotsPerBurst)
	for i := range frames {
		frames[i] = vocoder.AMBESilence()
	}
	voice, err := tx.AddFrames(frames...)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, voice...)
	tail, err := tx.Terminate()
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, tail...)
	var out []timedBurst
	n := 0
	for _, r := range raw {
		b, err := layer2.NewBurstFromBytes(r)
		if err != nil {
			t.Fatal(err)
		}
		if b.Kind == layer2.KindData {
			dt := b.SlotType.DataType
			out = append(out, timedBurst{control: mmdvm.ControlDataSync | mmdvm.Control(dt), frame: dmrconst.FrameDataSync, value: uint(dt), burst: r})
			continue
		}
		c, ft := mmdvm.Control(n), dmrconst.FrameVoice
		if n == 0 {
			c, ft = mmdvm.ControlVoiceSync, dmrconst.FrameVoiceSync
		}
		out = append(out, timedBurst{control: c, frame: ft, value: uint(n), burst: r})
		n = (n + 1) % 6
	}
	return out
}

func (b *bench) playRF(s enums.Slot, call []timedBurst) {
	for _, c := range call {
		b.rx(s, c.control, c.burst)
		b.run(2 * 30 * time.Millisecond)
	}
}

func (b *bench) playNetwork(s enums.Slot, stream uint, call []timedBurst) {
	for i, c := range call {
		p := models.Packet{Signature: "DMRD", Seq: uint(i), Src: benchRadio, Dst: benchTG, Repeater: benchRepeater,
			Slot: s == enums.Slot2, GroupCall: true, FrameType: c.frame, DTypeOrVSeq: c.value, StreamID: stream, DMRData: c.burst}
		b.r.packet(b.now, p)
		b.run(2 * 30 * time.Millisecond)
	}
}

func checkRouted(t *testing.T, got []models.Packet, call []timedBurst, slot bool) {
	t.Helper()
	if len(got) < len(call) {
		t.Fatalf("the hub got %d packets of a call of %d bursts", len(got), len(call))
	}
	stream := got[0].StreamID
	for i, p := range got {
		if p.Src != benchRadio || p.Dst != benchTG || !p.GroupCall || p.Repeater != benchRepeater || p.Slot != slot ||
			p.StreamID != stream || stream == 0 || p.Seq != uint(i%256) {
			t.Fatalf("packet %d: %s", i, p.String())
		}
	}
	if got[0].FrameType != dmrconst.FrameDataSync || got[0].DTypeOrVSeq != uint(enums.DataTypeVoiceLCHeader) {
		t.Fatalf("the first packet is not the Voice LC Header: %s", got[0].String())
	}
	last := got[len(got)-1]
	if last.FrameType != dmrconst.FrameDataSync || last.DTypeOrVSeq != uint(enums.DataTypeTerminatorWithLC) {
		t.Fatalf("the last packet is not a Terminator: %s", last.String())
	}
	voice, n := 0, 0
	for _, p := range got {
		switch p.FrameType { //nolint:exhaustive // the voice packets
		case dmrconst.FrameVoiceSync:
			if n != 0 {
				t.Fatalf("voice sync at position %d", n)
			}
			voice++
			n = 1
		case dmrconst.FrameVoice:
			if p.DTypeOrVSeq != uint(n) {
				t.Fatalf("voice position %d, want %d", p.DTypeOrVSeq, n)
			}
			voice++
			n = (n + 1) % 6
		}
	}
	want := 0
	for _, c := range call {
		if c.frame != dmrconst.FrameDataSync {
			want++
		}
	}
	if voice != want {
		t.Fatalf("the hub got %d voice packets, the radio sent %d", voice, want)
	}
}

func TestSimplexRFCallGoesToTheHubOnTS2(t *testing.T) {
	t.Parallel()
	b := newBench(t, false, 1)
	b.run(time.Second)
	call := voiceCall(t, layer2.SyncSourceMS, 3)
	b.playRF(enums.Slot2, call)
	b.run(time.Second)
	checkRouted(t, b.routed, call, true)
	if n := b.count(mmdvm.CommandDMRData2) + b.count(mmdvm.CommandDMRData1); n != 0 {
		t.Fatalf("a simplex hotspot repeated %d RF bursts", n)
	}
	if b.count(mmdvm.CommandDMRStart) != 0 {
		t.Fatal("a simplex hotspot sent DMR_START")
	}
	if c := b.control(); len(c) == 0 || c[0].Command != mmdvm.CommandSetMode || c[0].Payload[0] != byte(mmdvm.ModeDMR) {
		t.Fatalf("the first RF burst did not set DMR mode: %v", c)
	}
}

func TestDuplexRFCallIsRepeatedAndGoesToTheHub(t *testing.T) {
	t.Parallel()
	b := newBench(t, true, 2)
	b.run(time.Second)
	call := voiceCall(t, layer2.SyncSourceMS, 3)
	b.playRF(enums.Slot1, call)
	b.run(time.Second)
	checkRouted(t, b.routed, call, false)
	if b.count(mmdvm.CommandDMRStart) == 0 {
		t.Fatal("the repeater did not key up")
	}
	rep := b.bursts(mmdvm.CommandDMRData1)
	if len(rep) < len(call) {
		t.Fatalf("the repeater repeated %d bursts of %d", len(rep), len(call))
	}
	if p, d := rep[0].Sync(); p != enums.SyncBSSourcedData || d != 0 {
		t.Fatalf("the repeated header has SYNC %v at %d", p, d)
	}
	if b.count(mmdvm.CommandDMRData2) != 0 {
		t.Fatal("a slot 1 call went out on slot 2")
	}
}

func TestSimplexNetworkCallGoesOutWithMSSyncAndTS1IsDropped(t *testing.T) {
	t.Parallel()
	b := newBench(t, false, 1)
	b.run(time.Second)
	call := voiceCall(t, layer2.SyncSourceBS, 2)
	b.playNetwork(enums.Slot1, 0x1111, call)
	b.run(time.Second)
	if n := b.count(mmdvm.CommandDMRData1) + b.count(mmdvm.CommandDMRData2); n != 0 {
		t.Fatalf("a simplex hotspot sent %d bursts of a TS1 call", n)
	}
	b.playNetwork(enums.Slot2, 0x2222, call)
	b.run(time.Second)
	out := b.bursts(mmdvm.CommandDMRData2)
	if len(out) < len(call) {
		t.Fatalf("the hotspot sent %d bursts of %d", len(out), len(call))
	}
	syncs := 0
	for i, o := range out {
		p, d := o.Sync()
		if d != 0 {
			continue
		}
		syncs++
		if p != enums.SyncMSSourcedData && p != enums.SyncMSSourcedVoice {
			t.Fatalf("burst %d has SYNC %v, want MS-sourced", i, p)
		}
	}
	if syncs == 0 {
		t.Fatal("no burst with a SYNC")
	}
	if len(b.routed) != 0 {
		t.Fatalf("a network call went back to the hub: %d packets", len(b.routed))
	}
	b.run(10 * time.Second)
	c := b.control()
	if last := c[len(c)-1]; last.Command != mmdvm.CommandSetMode || last.Payload[0] != byte(mmdvm.ModeIdle) {
		t.Fatalf("the hotspot did not go idle after the call: %v", last)
	}
}

func TestDuplexNetworkCallSetsTheShortLC(t *testing.T) {
	t.Parallel()
	b := newBench(t, true, 2)
	b.run(time.Second)
	call := voiceCall(t, layer2.SyncSourceBS, 2)
	b.playNetwork(enums.Slot2, 0x3333, call[:len(call)/2])
	hash, err := pdu.AddressHash(benchTG)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, f := range b.sent {
		if f.Command != mmdvm.CommandDMRShortLC {
			continue
		}
		s, err := mmdvm.DecodeDMRShortLC(f.Payload)
		if err != nil {
			t.Fatal(err)
		}
		slc := s.ShortLC()
		if au, ok := slc.AsActivityUpdate(); ok && au.TS2ActivityID == enums.ActivityGroupVoice && au.HashTS2 == hash &&
			au.TS1ActivityID == enums.ActivityNoActivity {
			seen = true
		}
	}
	if !seen {
		t.Fatal("no Activity Update for the TS2 group call")
	}
	b.playNetwork(enums.Slot2, 0x3333, call[len(call)/2:])
	b.run(10 * time.Second)
	if n, out := b.count(mmdvm.CommandDMRStart), len(b.bursts(mmdvm.CommandDMRData2)); n != 2 || out < len(call) || len(b.bursts(mmdvm.CommandDMRData1)) != 0 {
		t.Fatalf("the repeater sent %d DMR_START and %d bursts of %d on slot 2", n, out, len(call))
	}
	c := b.control()
	last := c[len(c)-2:]
	if last[0].Command != mmdvm.CommandDMRStart || last[0].Payload[0] != 0 || last[1].Command != mmdvm.CommandSetMode ||
		last[1].Payload[0] != byte(mmdvm.ModeIdle) {
		t.Fatalf("the repeater did not end with DMR_START 0 and idle: %v %v", last[0], last[1])
	}
	var final mmdvm.Frame
	for _, f := range c {
		if f.Command == mmdvm.CommandDMRShortLC {
			final = f
		}
	}
	s, err := mmdvm.DecodeDMRShortLC(final.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if slc := s.ShortLC(); slc.SLCO != enums.SLCONullMessage {
		t.Fatalf("the last Short LC is %v", slc.SLCO)
	}
}

func TestRadioIsSilentWithoutStatus(t *testing.T) {
	t.Parallel()
	b := newBench(t, true, 2)
	b.run(time.Second)
	if b.r.silent(b.now) {
		t.Fatal("silent while the modem answers")
	}
	b.silent = true
	b.run(silenceLimit - 300*time.Millisecond)
	if b.r.silent(b.now) {
		t.Fatal("silent too soon")
	}
	b.run(400 * time.Millisecond)
	if !b.r.silent(b.now) {
		t.Fatal("not silent 2 s after the last status")
	}
}
