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
	"math"
	"testing"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
)

const (
	benchAlias = "N6CALL Jacob M"
	benchLat   = 32.7767
	benchLon   = -96.797
)

// embeddedCall returns the bursts of a group voice call from benchRadio
// that sends the talker alias benchAlias and then the GPS position of
// benchLat and benchLon in the embedded LC of its superframes.
func embeddedCall(t *testing.T) []timedBurst {
	t.Helper()
	lcs, err := pdu.EncodeTalkerAlias(benchAlias, layer3.TalkerAliasISOEightBit)
	if err != nil {
		t.Fatal(err)
	}
	var gps pdu.FullLinkControl
	if err := gps.SetGPSInfo(pdu.FLCGPSInfo{Latitude: benchLat, Longitude: benchLon}); err != nil {
		t.Fatal(err)
	}
	return embeddedCallFrom(t, benchRadio, append(lcs, gps))
}

// embeddedCallFrom returns the bursts of a group voice call from source
// that sends lcs in the embedded LC of its superframes.
func embeddedCallFrom(t *testing.T, source uint32, lcs []pdu.FullLinkControl) []timedBurst {
	t.Helper()
	var lcb pdu.LCBytes
	if err := lcb.SetFLCO(enums.FLCOGroupVoiceChannelUser); err != nil {
		t.Fatal(err)
	}
	if err := lcb.SetCall(true, benchTG, source); err != nil {
		t.Fatal(err)
	}
	tx, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{
		LC: pdu.DecodeFullLinkControlBytes(lcb), ColorCode: 1, Source: layer2.SyncSourceMS,
		EmbeddedLC: func(n int) *pdu.FullLinkControl {
			if n < len(lcs) {
				return &lcs[n]
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := tx.Headers()
	frames := make([]vocoder.AMBEFrame, (len(lcs)+1)*6*layer2.VoiceSlotsPerBurst)
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
			out = append(out, timedBurst{control: mmdvm.ControlDataSync | mmdvm.Control(b.SlotType.DataType), burst: r})
			continue
		}
		c := mmdvm.Control(n)
		if n == 0 {
			c = mmdvm.ControlVoiceSync
		}
		out = append(out, timedBurst{control: c, burst: r})
		n = (n + 1) % 6
	}
	return out
}

// voiceBurst returns the burst at position pos (0 for A) of superframe sf
// of call.
func voiceBurst(t *testing.T, call []timedBurst, sf, pos int) *layer2.RawBurst {
	t.Helper()
	i := 0
	for j := range call {
		if call[j].control&mmdvm.ControlDataSync != 0 {
			continue
		}
		if i == sf*6+pos {
			return &call[j].burst
		}
		i++
	}
	t.Fatalf("the call has no burst %d of superframe %d", pos, sf)
	return nil
}

// flipEmbeddedBit flips one bit of the embedded signalling field of burst B
// of superframe sf of call.
func flipEmbeddedBit(t *testing.T, call []timedBurst, sf int) {
	t.Helper()
	b := voiceBurst(t, call, sf, 1)
	d := b.EmbeddedData()
	d[1] ^= 0x10
	b.SetEmbeddedData(d)
}

// shortAlias returns the embedded LCs that carry a talker alias short
// enough for its header block alone.
func shortAlias(t *testing.T) []pdu.FullLinkControl {
	t.Helper()
	lcs, err := pdu.EncodeTalkerAlias("N0CALL", layer3.TalkerAliasISOEightBit)
	if err != nil {
		t.Fatal(err)
	}
	if len(lcs) != 1 {
		t.Fatalf("the alias takes %d blocks, want 1", len(lcs))
	}
	return lcs
}

func TestRFTalkerAliasAndGPSGoToTheHub(t *testing.T) {
	t.Parallel()
	b := newBench(t, false, 1)
	b.run(time.Second)
	b.playRF(enums.Slot2, embeddedCall(t))
	b.run(time.Second)
	if len(b.aliases) == 0 {
		t.Fatal("no talker alias block went to the hub")
	}
	var asm pdu.TalkerAliasAssembler
	var text string
	for i, a := range b.aliases {
		if a.Repeater != benchRepeater || a.Source != benchRadio || a.Size != talkerAliasSize {
			t.Fatalf("alias block %d: %+v", i, a)
		}
		lc, err := a.LC()
		if err != nil {
			t.Fatal(err)
		}
		if s, ok, err := asm.Add(&lc); err == nil && ok {
			text = s
		}
	}
	if text != benchAlias {
		t.Fatalf("the hub got the alias %q, want %q", text, benchAlias)
	}
	if len(b.positions) != 1 {
		t.Fatalf("the hub got %d positions, want 1", len(b.positions))
	}
	p := b.positions[0]
	if p.Repeater != benchRepeater || p.Source != benchRadio || p.Size != positionSize {
		t.Fatalf("position %+v", p)
	}
	lc, err := p.LC()
	if err != nil {
		t.Fatal(err)
	}
	g, ok := lc.AsGPSInfo()
	if !ok || math.Abs(g.Latitude-benchLat) > 0.001 || math.Abs(g.Longitude-benchLon) > 0.001 {
		t.Fatalf("the hub got the position %+v (%t)", g, ok)
	}
}

func TestTalkerAliasOfACallTheHotspotDropsStaysOff(t *testing.T) {
	t.Parallel()
	b := newBench(t, false, 1)
	b.run(time.Second)
	b.playRF(enums.Slot1, embeddedCall(t))
	b.run(time.Second)
	if len(b.aliases) != 0 || len(b.positions) != 0 {
		t.Fatalf("a TS1 call on a simplex hotspot sent %d alias blocks and %d positions", len(b.aliases), len(b.positions))
	}
}

func TestACorrectedAliasBlockOfANewCallNeedsItsOwnConfirmation(t *testing.T) {
	t.Parallel()
	b := newBench(t, false, 1)
	b.run(time.Second)
	b.playRF(enums.Slot2, embeddedCallFrom(t, benchRadio, shortAlias(t)))
	b.run(time.Second)
	if len(b.aliases) != 1 {
		t.Fatalf("the first call sent %d alias blocks, want 1", len(b.aliases))
	}
	second := embeddedCallFrom(t, benchRadio+1, shortAlias(t))
	flipEmbeddedBit(t, second, 0)
	b.playRF(enums.Slot2, second)
	b.run(time.Second)
	if len(b.aliases) != 1 {
		t.Fatalf("a block with a corrected bit in a new call went to the hub: %+v", b.aliases[1:])
	}
}

func TestABurstOfAnotherColorCodeKeepsTheAliasHistory(t *testing.T) {
	t.Parallel()
	alias := shortAlias(t)
	var other pdu.FullLinkControl
	if err := other.SetGPSInfo(pdu.FLCGPSInfo{Latitude: benchLat, Longitude: benchLon}); err != nil {
		t.Fatal(err)
	}
	call := embeddedCallFrom(t, benchRadio, []pdu.FullLinkControl{alias[0], other, alias[0]})
	bb := voiceBurst(t, call, 1, 1)
	emb := bb.EMB()
	emb.ColorCode = 2
	if err := bb.SetEMB(emb); err != nil {
		t.Fatal(err)
	}
	flipEmbeddedBit(t, call, 2)
	b := newBench(t, false, 1)
	b.run(time.Second)
	b.playRF(enums.Slot2, call)
	b.run(time.Second)
	if len(b.aliases) != 2 {
		t.Fatalf("the hub got %d alias blocks, want 2", len(b.aliases))
	}
	if len(b.positions) != 0 {
		t.Fatalf("the hub got %d positions from a superframe with a burst of another color code", len(b.positions))
	}
}
