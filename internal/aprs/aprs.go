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

// Package aprs builds the packets that DMRHub sends to APRS-IS.
package aprs

import (
	"errors"
	"fmt"
	"strings"
)

// ToCall is the destination of every packet DMRHub sends to APRS-IS. It
// names the software, so every DMRHub instance sends the same one. APZDMH is
// in the experimental APZ range until a tocall is assigned in the APRS
// device ID list.
const ToCall = "APZDMH"

// Config is the APRS-IS login of a DMRHub instance.
type Config struct {
	// Login is the callsign with SSID that the hub logs in to APRS-IS as.
	Login string
	// Passcode is the APRS-IS passcode of Login.
	Passcode string
	// Server is the APRS-IS server, as host:port.
	Server string
}

// ErrCallsign is returned for a callsign that cannot go in an APRS header.
var ErrCallsign = errors.New("invalid APRS callsign")

// Line returns the TNC2 line of a position from the user source heard on
// the repeater or hotspot via, in the IGate form that BrandMeister's gateway
// uses: source>ToCall,via*,qAR,Login:info.
func (c Config) Line(source, via, info string) (string, error) {
	for _, call := range []string{source, via, c.Login} {
		if !validCallsign(call) {
			return "", fmt.Errorf("%w: %q", ErrCallsign, call)
		}
	}
	if strings.ContainsAny(info, "\r\n") {
		return "", errors.New("APRS information field holds a line break")
	}
	return source + ">" + ToCall + "," + via + "*,qAR," + c.Login + ":" + info, nil
}

func validCallsign(call string) bool {
	base, ssid, hasSSID := strings.Cut(call, "-")
	if base == "" || len(base) > 9 || (hasSSID && (ssid == "" || len(ssid) > 2)) {
		return false
	}
	for _, r := range base + ssid {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// Licensed reports whether call looks like the callsign of a licensed
// amateur: letters and digits with at least one of each, without an SSID.
func Licensed(call string) bool {
	if strings.Contains(call, "-") || !validCallsign(call) {
		return false
	}
	return strings.ContainsAny(call, "0123456789") && strings.ContainsAny(call, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}
