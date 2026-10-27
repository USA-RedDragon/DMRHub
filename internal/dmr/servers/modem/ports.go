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
	"strings"
)

// SerialPort is a serial device that a modem can be on.
type SerialPort struct {
	// Path is the name to put in the config: a stable link such as
	// /dev/serial/by-id/usb-... when there is one, else the device.
	Path string `json:"path"`
	// Device is the device that Path names, when Path is a link.
	Device string `json:"device,omitempty"`
}

// SerialPorts returns the serial ports of this host: the links of
// /dev/serial/by-id and the /dev/serial0 and /dev/serial1 links of a
// Raspberry Pi first, then the ttyACM, ttyUSB, ttyAMA and ttyS devices
// that no link names. It lists a ttyS device only when the kernel found a
// UART behind it.
func SerialPorts() []SerialPort {
	return serialPorts("/dev", "/sys/class/tty")
}

func serialPorts(dev, sys string) []SerialPort {
	out := []SerialPort{}
	named := map[string]bool{}
	link := func(path string) {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return
		}
		out = append(out, SerialPort{Path: path, Device: target})
		named[target] = true
	}
	byID, _ := filepath.Glob(filepath.Join(dev, "serial", "by-id", "*"))
	slices.Sort(byID)
	for _, p := range byID {
		link(p)
	}
	for _, p := range []string{"serial0", "serial1"} {
		if _, err := os.Lstat(filepath.Join(dev, p)); err == nil {
			link(filepath.Join(dev, p))
		}
	}
	for _, g := range []string{"ttyACM*", "ttyUSB*", "ttyAMA*", "ttyS*"} {
		devs, _ := filepath.Glob(filepath.Join(dev, g))
		slices.SortFunc(devs, byNumber)
		for _, d := range devs {
			if named[d] || strings.HasPrefix(filepath.Base(d), "ttyS") && !hasUART(sys, filepath.Base(d)) {
				continue
			}
			out = append(out, SerialPort{Path: d})
		}
	}
	return out
}

// hasUART reports whether the kernel found a UART behind the ttyS device
// name, from its type in sysfs, which is 0 for none.
func hasUART(sys, name string) bool {
	b, err := os.ReadFile(filepath.Join(sys, name, "type")) //nolint:gosec // a file of sysfs
	if err != nil {
		return false
	}
	t := strings.TrimSpace(string(b))
	return t != "" && t != "0"
}

func byNumber(a, b string) int {
	ta, na := split(a)
	tb, nb := split(b)
	if c := strings.Compare(ta, tb); c != 0 {
		return c
	}
	if len(na) != len(nb) {
		return len(na) - len(nb)
	}
	return strings.Compare(na, nb)
}

func split(s string) (string, string) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	return s[:i], s[i:]
}
