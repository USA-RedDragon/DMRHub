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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

const rssiFile = `# This file maps the raw RSSI values to dBm values to send to the DMR network. A number of data
# points should be entered and the software will use those to work out the in-between values.
#
# The format of the file is:
# Raw RSSI Value		dBm Value
#
1134		-90
1123	-100
1000 -109
1000 -50
not a point
1100
`

func TestRSSIMapInterpolatesAsMMDVMHost(t *testing.T) {
	t.Parallel()
	m, err := parseRSSIMap(strings.NewReader(rssiFile))
	if err != nil {
		t.Fatal(err)
	}
	if m.points() != 3 {
		t.Fatalf("%d points, want 3", m.points())
	}
	for _, c := range []struct {
		raw  uint16
		want int
	}{
		{1134, -90}, {2000, -90}, {1123, -100}, {1000, -109}, {500, -109}, {1050, -105}, {1130, -93},
	} {
		got, ok := m.dBm(c.raw)
		if !ok || got != c.want {
			t.Errorf("raw %d: %d dBm (%t), want %d", c.raw, got, ok, c.want)
		}
	}
	var empty rssiMap
	if _, ok := empty.dBm(1000); ok {
		t.Fatal("an empty map gave an RSSI")
	}
}

func TestLoadRSSIMapReadsTheFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "RSSI.dat")
	if err := os.WriteFile(path, []byte(rssiFile), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := loadRSSIMap(path)
	if err != nil || m.points() != 3 {
		t.Fatalf("%d points, %v", m.points(), err)
	}
	if _, err := loadRSSIMap(filepath.Join(t.TempDir(), "missing.dat")); err == nil {
		t.Fatal("a missing file loaded")
	}
}

func TestRFCallCarriesTheRSSIInDBm(t *testing.T) {
	t.Parallel()
	m, err := parseRSSIMap(strings.NewReader(rssiFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		rssi rssiMap
		want int
	}{{"mapped", m, 105}, {"no file", rssiMap{}, -1}} {
		b := newBenchRSSI(t, false, 1, c.rssi)
		b.run(time.Second)
		call := voiceCall(t, layer2.SyncSourceMS, 2)
		for _, tb := range call {
			b.rxRSSI(enums.Slot2, tb.control, tb.burst, 1050)
			b.run(2 * 30 * time.Millisecond)
		}
		b.run(time.Second)
		checkRouted(t, b.routed, call, true)
		for i, p := range b.routed {
			if p.RSSI != c.want {
				t.Fatalf("%s: packet %d has RSSI %d, want %d", c.name, i, p.RSSI, c.want)
			}
		}
	}
}

func (b *bench) rxRSSI(s enums.Slot, c mmdvm.Control, burst layer2.RawBurst, raw uint16) {
	d := mmdvm.DMRData{Slot: s, Control: c, Burst: burst, RSSI: raw, HasRSSI: true}
	p, err := d.AppendBinary(nil)
	if err != nil {
		b.t.Fatal(err)
	}
	b.r.frame(b.now, mmdvm.Frame{Command: d.Command(), Payload: p})
}
