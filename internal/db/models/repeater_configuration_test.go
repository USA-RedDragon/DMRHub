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
	"math"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// buildValidRPTCPacket builds a 302-byte RPTC config packet with plausible values.
// The layout mirrors what a real MMDVM repeater sends.
func buildValidRPTCPacket() []byte {
	data := make([]byte, 302)
	copy(data[0:4], "RPTC")
	// bytes 4-7: repeater ID (big-endian)
	data[4] = 0x00
	data[5] = 0x04
	data[6] = 0xB0
	data[7] = 0x01 // 307201
	// bytes 8-15: callsign (8 chars, space-padded)
	copy(data[8:16], "N0CALL  ")
	// bytes 16-24: RX frequency (9 chars, space-padded)
	copy(data[16:25], "145000000")
	// bytes 25-33: TX frequency (9 chars, space-padded)
	copy(data[25:34], "145600000")
	// bytes 34-35: TX power (2 chars)
	copy(data[34:36], "50")
	// bytes 36-37: color code (2 chars)
	copy(data[36:38], "01")
	// bytes 38-45: latitude (8 chars)
	copy(data[38:46], "35.0000 ")
	// bytes 46-54: longitude (9 chars)
	copy(data[46:55], "-97.0000 ")
	// bytes 55-57: height (3 chars)
	copy(data[55:58], "100")
	// bytes 58-77: location (20 chars)
	loc := "Oklahoma City       "
	copy(data[58:78], loc)
	// bytes 78-96: description (19 chars)
	desc := "DMRHub Test        "
	copy(data[78:97], desc)
	// byte 97: slots
	data[97] = '4'
	// bytes 98-221: URL (124 chars, space-padded)
	url := "https://example.com"
	copy(data[98:222], url)
	for i := 98 + len(url); i < 222; i++ {
		data[i] = ' '
	}
	// bytes 222-261: software ID (40 chars)
	for i := 222; i < 262; i++ {
		data[i] = ' '
	}
	// bytes 262-301: package ID (40 chars)
	for i := 262; i < 302; i++ {
		data[i] = ' '
	}
	return data
}

func FuzzParseConfig(f *testing.F) {
	// Seed with a valid 302-byte RPTC config packet
	f.Add(buildValidRPTCPacket())
	// All zeros
	f.Add(make([]byte, 302))
	// All 0xFF
	maxed := make([]byte, 302)
	for i := range maxed {
		maxed[i] = 0xFF
	}
	f.Add(maxed)
	// All spaces
	spaced := make([]byte, 302)
	for i := range spaced {
		spaced[i] = ' '
	}
	f.Add(spaced)

	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()
		if len(data) < 302 {
			return // ParseConfig expects exactly 302 bytes
		}
		var c models.RepeaterConfiguration
		// Errors are expected for invalid data, but must not panic
		_ = c.ParseConfig(data[:302], "test", "abc123")
	})
}

func FuzzRepeaterConfigurationUnmarshalMsg(f *testing.F) {
	good := models.RepeaterConfiguration{
		Callsign:    "N0CALL",
		ID:          307201,
		RXFrequency: 145000000,
		TXFrequency: 145600000,
		TXPower:     50,
		ColorCode:   1,
		Latitude:    35.0,
		Longitude:   -97.0,
		Height:      100,
		Location:    "Oklahoma City",
		Description: "Test",
		Slots:       4,
	}
	encoded, err := good.MarshalMsg(nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()
		var c models.RepeaterConfiguration
		_, _ = c.UnmarshalMsg(data)
	})
}

func TestParseConfigKnownGoodPacket(t *testing.T) {
	t.Parallel()
	data := buildValidRPTCPacket()
	var c models.RepeaterConfiguration
	err := c.ParseConfig(data, "test", "abc123")
	if err != nil {
		t.Fatalf("ParseConfig returned unexpected error: %v", err)
	}

	if c.Callsign != "N0CALL" {
		t.Errorf("Callsign = %q, want %q", c.Callsign, "N0CALL")
	}
	if c.RXFrequency != 145000000 {
		t.Errorf("RXFrequency = %d, want %d", c.RXFrequency, 145000000)
	}
	if c.TXFrequency != 145600000 {
		t.Errorf("TXFrequency = %d, want %d", c.TXFrequency, 145600000)
	}
	if c.TXPower != 50 {
		t.Errorf("TXPower = %d, want %d", c.TXPower, 50)
	}
	if c.ColorCode != 1 {
		t.Errorf("ColorCode = %d, want %d", c.ColorCode, 1)
	}
	// ParseFloat with bitSize=32 rounds to float32 precision, then stores as float64.
	if math.Abs(c.Latitude-35.0) > 0.001 {
		t.Errorf("Latitude = %f, want ~35.0", c.Latitude)
	}
	if math.Abs(c.Longitude-(-97.0)) > 0.001 {
		t.Errorf("Longitude = %f, want ~-97.0", c.Longitude)
	}
	if c.Height != 100 {
		t.Errorf("Height = %d, want %d", c.Height, 100)
	}
	if c.Location != "Oklahoma City" {
		t.Errorf("Location = %q, want %q", c.Location, "Oklahoma City")
	}
	if c.Description != "DMRHub Test" {
		t.Errorf("Description = %q, want %q", c.Description, "DMRHub Test")
	}
	if c.Slots != 4 {
		t.Errorf("Slots = %d, want %d", c.Slots, 4)
	}
	if c.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", c.URL, "https://example.com")
	}
	// SoftwareID and PackageID are empty in the packet, so defaults apply.
	wantSW := "USA-RedDragon/DMRHub test-abc123"
	if c.SoftwareID != wantSW {
		t.Errorf("SoftwareID = %q, want %q", c.SoftwareID, wantSW)
	}
}

func BenchmarkParseConfig(b *testing.B) {
	data := buildValidRPTCPacket()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var c models.RepeaterConfiguration
		_ = c.ParseConfig(data, "test", "abc123")
	}
}

func BenchmarkRepeaterConfigurationMarshalMsg(b *testing.B) {
	c := models.RepeaterConfiguration{
		Callsign:    "N0CALL",
		ID:          307201,
		RXFrequency: 145000000,
		TXFrequency: 145600000,
		TXPower:     50,
		ColorCode:   1,
		Latitude:    35.0,
		Longitude:   -97.0,
		Height:      100,
		Location:    "Oklahoma City",
		Description: "Test",
		Slots:       4,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.MarshalMsg(nil)
	}
}

func BenchmarkRepeaterConfigurationUnmarshalMsg(b *testing.B) {
	c := models.RepeaterConfiguration{
		Callsign:    "N0CALL",
		ID:          307201,
		RXFrequency: 145000000,
		TXFrequency: 145600000,
		TXPower:     50,
		ColorCode:   1,
		Latitude:    35.0,
		Longitude:   -97.0,
		Height:      100,
		Location:    "Oklahoma City",
		Description: "Test",
		Slots:       4,
	}
	encoded, err := c.MarshalMsg(nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dec models.RepeaterConfiguration
		_, _ = dec.UnmarshalMsg(encoded)
	}
}
