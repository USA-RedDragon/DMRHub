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

package models_test

import (
	"errors"
	"math"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

type rptcFields struct {
	callsign, rx, tx, power, cc, lat, lon, height, location, description, slots, url, software, pkg string
}

func (f rptcFields) packet(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 302)
	copy(data, "RPTC")
	data[4], data[5], data[6], data[7] = 0x13, 0x05, 0x2c, 0x15
	off := 8
	for _, field := range []struct {
		v string
		n int
	}{
		{f.callsign, 8}, {f.rx, 9}, {f.tx, 9}, {f.power, 2}, {f.cc, 2}, {f.lat, 8}, {f.lon, 9}, {f.height, 3},
		{f.location, 20}, {f.description, 19}, {f.slots, 1}, {f.url, 124}, {f.software, 40}, {f.pkg, 40},
	} {
		if len(field.v) != field.n {
			t.Fatalf("field %q at %d has %d octets, want %d", field.v, off, len(field.v), field.n)
		}
		copy(data[off:], field.v)
		off += field.n
	}
	return data
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func lpad(s string, n int) string {
	for len(s) < n {
		s = " " + s
	}
	return s
}

func TestParseConfigClientPadding(t *testing.T) {
	t.Parallel()
	type want struct {
		rx, tx          uint
		power, cc       uint8
		lat, lon        float64
		height          uint16
		slots           uint
		location, url   string
		software, pkg   string
		callsign, descr string
	}
	cases := []struct {
		name string
		f    rptcFields
		want want
	}{
		{
			// hblink4/hblink.py _send_outbound_config with utils.py pad_*:
			// numbers zero filled, latitude and longitude right-justified with spaces.
			name: "hblink4",
			f: rptcFields{
				callsign: pad("KI5VMF", 8), rx: "449000000", tx: "444000000", power: "01", cc: "01",
				lat: lpad("30.2672", 8), lon: lpad("-97.7431", 9), height: "000",
				location: pad("Austin", 20), description: pad("HBlink4", 19), slots: "3",
				url: pad("", 124), software: pad("HBlink4", 40), pkg: pad("HBlink4 v2.0", 40),
			},
			want: want{rx: 449000000, tx: 444000000, power: 1, cc: 1, lat: 30.2672, lon: -97.7431, slots: 3,
				location: "Austin", software: "HBlink4", pkg: "HBlink4 v2.0", callsign: "KI5VMF", descr: "HBlink4"},
		},
		{
			name: "hblink4 short float",
			f: rptcFields{
				callsign: pad("KI5VMF", 8), rx: "000000000", tx: "000000000", power: "00", cc: "01",
				lat: lpad("30.0", 8), lon: lpad("0.0", 9), height: "075",
				location: pad("", 20), description: pad("", 19), slots: "3",
				url: pad("", 124), software: pad("HBlink4", 40), pkg: pad("HBlink4 v2.0", 40),
			},
			want: want{cc: 1, lat: 30, height: 75, slots: 3, software: "HBlink4", pkg: "HBlink4 v2.0", callsign: "KI5VMF"},
		},
		{
			// DMRGateway.cpp:1321-1328 (MMDVMHost, YSF2DMR and DroidStar use the
			// same sprintf): "%08f" and "%09f" cut to 8 and 9 characters.
			name: "dmrgateway",
			f: rptcFields{
				callsign: pad("KI5VMF", 8), rx: "449000000", tx: "444000000", power: "01", cc: "01",
				lat: "30.26720", lon: "-97.74310", height: "000",
				location: pad("Austin, TX", 20), description: pad("MMDVM", 19), slots: "4",
				url: pad("https://example.com", 124), software: pad("20250101", 40), pkg: pad("MMDVM", 40),
			},
			want: want{rx: 449000000, tx: 444000000, power: 1, cc: 1, lat: 30.2672, lon: -97.7431, slots: 4,
				location: "Austin, TX", url: "https://example.com", software: "20250101", pkg: "MMDVM",
				callsign: "KI5VMF", descr: "MMDVM"},
		},
		{
			name: "ysf2dmr zero position",
			f: rptcFields{
				callsign: pad("KI5VMF", 8), rx: "000000000", tx: "000000000", power: "01", cc: "01",
				lat: "0.000000", lon: "00.000000", height: "000",
				location: pad("", 20), description: pad("", 19), slots: "4",
				url: pad("", 124), software: pad("20180101", 40), pkg: pad("MMDVM", 40),
			},
			want: want{power: 1, cc: 1, slots: 4, software: "20180101", pkg: "MMDVM", callsign: "KI5VMF"},
		},
		{
			// hblink3/config.py:169-182: frequencies ljust(9), lat/lon ljust,
			// power, color code and height rjust with zeros.
			name: "hblink3 peer",
			f: rptcFields{
				callsign: pad("KI5VMF", 8), rx: pad("0", 9), tx: pad("0", 9), power: "25", cc: "01",
				lat: pad("38.0000", 8), lon: pad("-095.0000", 9), height: "075",
				location: pad("Anywhere, USA", 20), description: pad("This is a cool repe", 19), slots: "1",
				url: pad("www.w1abc.org", 124), software: pad("20170620", 40), pkg: pad("MMDVM_HBlink", 40),
			},
			want: want{power: 25, cc: 1, lat: 38, lon: -95, height: 75, slots: 1, location: "Anywhere, USA",
				url: "www.w1abc.org", software: "20170620", pkg: "MMDVM_HBlink", callsign: "KI5VMF",
				descr: "This is a cool repe"},
		},
		{
			name: "space padded numbers and text",
			f: rptcFields{
				callsign: lpad("KI5VMF", 8), rx: lpad("449000", 9), tx: pad("444000", 9), power: " 5", cc: " 7",
				lat: "\x00\x00\x00\x00\x00\x00\x00\x00", lon: "\x00\x00\x00\x00\x00\x00\x00\x00\x00", height: " 12",
				location: lpad("Austin", 20), description: lpad("Desc", 19), slots: " ",
				url: lpad("u", 124), software: lpad("sw", 40), pkg: lpad("pkg", 40),
			},
			want: want{rx: 449000, tx: 444000, power: 5, cc: 7, height: 12, location: "Austin", url: "u",
				software: "sw", pkg: "pkg", callsign: "KI5VMF", descr: "Desc"},
		},
		{
			name: "unparsable optional fields",
			f: rptcFields{
				callsign: pad("KI5VMF", 8), rx: "449.00000", tx: "abcdefghi", power: "xx", cc: "01",
				lat: "garbage ", lon: "NaN      ", height: "-05",
				location: pad("", 20), description: pad("", 19), slots: "x",
				url: pad("", 124), software: pad("sw", 40), pkg: pad("pkg", 40),
			},
			want: want{cc: 1, software: "sw", pkg: "pkg", callsign: "KI5VMF"},
		},
		{
			name: "position out of range",
			f: rptcFields{
				callsign: pad("KI5VMF", 8), rx: "449000000", tx: "444000000", power: "01", cc: "01",
				lat: "95.00000", lon: "-181.0000", height: "000",
				location: pad("", 20), description: pad("", 19), slots: "4",
				url: pad("", 124), software: pad("sw", 40), pkg: pad("pkg", 40),
			},
			want: want{rx: 449000000, tx: 444000000, power: 1, cc: 1, slots: 4, software: "sw", pkg: "pkg", callsign: "KI5VMF"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c models.RepeaterConfiguration
			if err := c.ParseConfig(tc.f.packet(t), "test", "abc123"); err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			got := want{
				rx: c.RXFrequency, tx: c.TXFrequency, power: c.TXPower, cc: c.ColorCode,
				height: c.Height, slots: c.Slots, location: c.Location, url: c.URL,
				software: c.SoftwareID, pkg: c.PackageID, callsign: c.Callsign, descr: c.Description,
				lat: tc.want.lat, lon: tc.want.lon,
			}
			if got != tc.want {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
			if math.Abs(c.Latitude-tc.want.lat) > 1e-4 || math.Abs(c.Longitude-tc.want.lon) > 1e-4 {
				t.Errorf("position = %v,%v, want %v,%v", c.Latitude, c.Longitude, tc.want.lat, tc.want.lon)
			}
		})
	}
}

func TestParseConfigStillChecksTheCallsign(t *testing.T) {
	t.Parallel()
	f := rptcFields{
		callsign: pad("", 8), rx: "449000000", tx: "444000000", power: "01", cc: "01",
		lat: "30.26720", lon: "-97.74310", height: "000",
		location: pad("", 20), description: pad("", 19), slots: "4",
		url: pad("", 124), software: pad("sw", 40), pkg: pad("pkg", 40),
	}
	var c models.RepeaterConfiguration
	if err := c.ParseConfig(f.packet(t), "test", "abc123"); !errors.Is(err, models.ErrInvalidCallsign) {
		t.Fatalf("ParseConfig error = %v, want %v", err, models.ErrInvalidCallsign)
	}
}

func TestParseConfigStoresZeroForABadColorCode(t *testing.T) {
	t.Parallel()
	for cc, want := range map[string]uint8{
		"x1": 0, "-1": 0, "16": 0, "99": 0, "  ": 0, "\x00\x00": 0,
		"00": 0, "01": 1, " 9": 9, "15": 15,
	} {
		t.Run(cc, func(t *testing.T) {
			t.Parallel()
			f := rptcFields{
				callsign: pad("KI5VMF", 8), rx: "449000000", tx: "444000000", power: "01", cc: cc,
				lat: "30.26720", lon: "-97.74310", height: "000",
				location: pad("", 20), description: pad("", 19), slots: "4",
				url: pad("", 124), software: pad("sw", 40), pkg: pad("pkg", 40),
			}
			c := models.RepeaterConfiguration{ColorCode: 7}
			if err := c.ParseConfig(f.packet(t), "test", "abc123"); err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			if c.ColorCode != want {
				t.Errorf("ColorCode = %d, want %d", c.ColorCode, want)
			}
			if c.Callsign != "KI5VMF" || c.RXFrequency != 449000000 || c.Slots != 4 {
				t.Errorf("the other fields were not read: %+v", c)
			}
		})
	}
}
