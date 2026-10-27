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
	"slices"
	"testing"
)

func TestSerialPortsListsStableNamesFirst(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dev, sys := filepath.Join(root, "dev"), filepath.Join(root, "sys")
	for _, d := range []string{"serial/by-id", "x"} {
		if err := os.MkdirAll(filepath.Join(dev, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"ttyACM0", "ttyACM10", "ttyACM2", "ttyUSB0", "ttyAMA0", "ttyS0", "ttyS1", "ttyS2", "tty0"} {
		if err := os.WriteFile(filepath.Join(dev, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for n, typ := range map[string]string{"ttyS0": "4\n", "ttyS1": "0\n"} {
		if err := os.MkdirAll(filepath.Join(sys, n), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sys, n, "type"), []byte(typ), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	byID := filepath.Join(dev, "serial/by-id/usb-MMDVM_HS-if00")
	if err := os.Symlink("../../ttyACM0", byID); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ttyAMA0", filepath.Join(dev, "serial0")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../gone", filepath.Join(dev, "serial/by-id/usb-gone-if00")); err != nil {
		t.Fatal(err)
	}
	got := serialPorts(dev, sys)
	want := []SerialPort{
		{Path: byID, Device: filepath.Join(dev, "ttyACM0")},
		{Path: filepath.Join(dev, "serial0"), Device: filepath.Join(dev, "ttyAMA0")},
		{Path: filepath.Join(dev, "ttyACM2")},
		{Path: filepath.Join(dev, "ttyACM10")},
		{Path: filepath.Join(dev, "ttyUSB0")},
		{Path: filepath.Join(dev, "ttyS0")},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestSerialPortsOfAHostWithNone(t *testing.T) {
	t.Parallel()
	if got := serialPorts(filepath.Join(t.TempDir(), "dev"), ""); got == nil || len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}
