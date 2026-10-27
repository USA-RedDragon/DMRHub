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
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

// rssiMap maps the raw RSSI that a modem reports to dBm, as MMDVMHost
// CRSSIInterpolator does with the points of an RSSI.dat file. The zero
// rssiMap has no points and gives no RSSI.
type rssiMap struct {
	raw []uint16
	dbm []int
}

// loadRSSIMap reads the RSSI.dat file at path.
func loadRSSIMap(path string) (rssiMap, error) {
	f, err := os.Open(path) //nolint:gosec // the path is a setting of the operator
	if err != nil {
		return rssiMap{}, fmt.Errorf("open the RSSI mapping file: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseRSSIMap(f)
}

// parseRSSIMap reads the points of an RSSI.dat file: a raw value and its
// dBm on each line, apart by spaces or tabs, and comment lines that start
// with #. The first point of a raw value counts, as in MMDVMHost. A line
// without two integers is not a point.
func parseRSSIMap(r io.Reader) (rssiMap, error) {
	var m rssiMap
	seen := make(map[uint16]bool)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.FieldsFunc(line, func(c rune) bool { return c == ' ' || c == '\t' || c == '\r' })
		if len(f) < 2 {
			continue
		}
		raw, err := strconv.ParseUint(f[0], 10, 16)
		if err != nil {
			continue
		}
		dbm, err := strconv.Atoi(f[1])
		if err != nil || seen[uint16(raw)] {
			continue
		}
		seen[uint16(raw)] = true
		i, _ := slices.BinarySearch(m.raw, uint16(raw))
		m.raw = slices.Insert(m.raw, i, uint16(raw))
		m.dbm = slices.Insert(m.dbm, i, dbm)
	}
	if err := sc.Err(); err != nil {
		return rssiMap{}, fmt.Errorf("read the RSSI mapping file: %w", err)
	}
	return m, nil
}

func (m rssiMap) points() int { return len(m.raw) }

// dBm returns the RSSI in dBm of the raw value v, interpolated between the
// points around it and held at the first and last point, as MMDVMHost
// CRSSIInterpolator::interpolate does. It returns false when m has no
// points.
func (m rssiMap) dBm(v uint16) (int, bool) {
	n := len(m.raw)
	if n == 0 {
		return 0, false
	}
	i, _ := slices.BinarySearch(m.raw, v)
	switch i {
	case n:
		return m.dbm[n-1], true
	case 0:
		return m.dbm[0], true
	}
	x1, y1, x2, y2 := m.raw[i-1], m.dbm[i-1], m.raw[i], m.dbm[i]
	p := float32(v-x1) / float32(x2-x1)
	return int((1-p)*float32(y1) + p*float32(y2)), true
}

// packetRSSI is the RSSI octet of a DMRD packet for raw, the RSSI in dBm
// as a positive number as MMDVMHost sends it, or -1 when it is not known.
func (m rssiMap) packetRSSI(raw uint16) int {
	dbm, ok := m.dBm(raw)
	if !ok || dbm >= 0 {
		return -1
	}
	return min(-dbm, maxRSSIOctet)
}
