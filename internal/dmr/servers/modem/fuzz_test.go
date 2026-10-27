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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

type nopSink struct{ frames, packets int }

func (n *nopSink) write([]byte)        { n.frames++ }
func (n *nopSink) route(models.Packet) { n.packets++ }

func (n *nopSink) alias(dmrd.TalkerAlias) { n.packets++ }

func (n *nopSink) position(dmrd.Position) { n.packets++ }

func fuzzSeeds(f *testing.F) {
	f.Helper()
	for _, name := range []string{"mmdvmhost.txt", "pistar.txt"} {
		var all []byte
		for _, c := range readCapture(f, name) {
			all = append(all, c.frame...)
			f.Add(byte(0), c.frame)
		}
		f.Add(byte(3), all)
	}
	for _, src := range []layer2.SyncSource{layer2.SyncSourceMS, layer2.SyncSourceBS} {
		var stream []byte
		for _, c := range voiceCall(f, src, 2) {
			d := mmdvm.DMRData{Slot: enums.Slot2, Control: c.control, Burst: c.burst}
			p, _ := d.AppendBinary(nil)
			fr, _ := mmdvm.AppendFrame(nil, d.Command(), p)
			stream = append(stream, fr...)
		}
		f.Add(byte(1), stream)
		f.Add(byte(2), stream)
	}
	f.Add(byte(0), []byte{0xE0, 0x00, 0x00, 0xE0, 0x03, 0x01, 0xE0, 0x00, 0xFF})
}

// FuzzModemInput feeds any octets from the modem, split as the link splits
// them, to a radio of each kind, with its clock running. Nothing may
// panic, and every frame the radio writes must be a whole frame.
func FuzzModemInput(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, kind byte, data []byte) {
		m := mmdvmhostModem()
		m.Duplex, m.ColorCode, m.Debug = kind&1 != 0, 1, kind&4 != 0
		protocol := uint8(1)
		if kind&2 != 0 {
			protocol = 2
		}
		var out nopSink
		now := time.Second
		r, err := newRadio(&m, 311860, protocol, &out, now, rssiMap{})
		if err != nil {
			t.Fatal(err)
		}
		var pending []byte
		for len(data) > 0 {
			n := min(len(data), int(kind>>3)+1)
			pending = append(pending, data[:n]...)
			data = data[n:]
			pending = splitFrames(pending, func(fr mmdvm.Frame) { r.frame(now, fr) })
			if len(pending) > mmdvm.MaxFrameLen {
				t.Fatalf("%d octets pending", len(pending))
			}
			now += 7 * time.Millisecond
			r.tick(now)
		}
		for range 100 {
			now += 30 * time.Millisecond
			r.tick(now)
		}
	})
}

// FuzzSplitFrames checks that splitFrames emits each frame that mmdvm.Next
// finds, keeps what is left, and never loops.
func FuzzSplitFrames(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, _ byte, data []byte) {
		p := append([]byte(nil), data...)
		total := 0
		left := splitFrames(p, func(fr mmdvm.Frame) { total += len(fr.Payload) })
		if len(left) > len(data) || len(left) >= mmdvm.MaxFrameLen {
			t.Fatalf("%d octets left of %d", len(left), len(data))
		}
		if total > len(data) {
			t.Fatalf("emitted %d payload octets of %d", total, len(data))
		}
	})
}
