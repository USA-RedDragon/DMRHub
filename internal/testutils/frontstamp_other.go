//go:build !linux

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

package testutils

import (
	"net"
	"time"
)

const stampOOBLen = 0

func stampTx(*net.UDPConn) bool { return false }

func sentAt(c *net.UDPConn, b []byte, addr *net.UDPAddr) (time.Time, bool) {
	_, _ = c.WriteToUDP(b, addr)
	return time.Time{}, false
}

func stampRx(*net.UDPConn) {}

func readStamped(c *net.UDPConn, b, _ []byte) (int, *net.UDPAddr, time.Time, bool, error) {
	n, from, err := c.ReadFromUDP(b)
	return n, from, time.Time{}, false, err //nolint:wrapcheck // the caller looks for net.ErrClosed
}
