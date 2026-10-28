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

package servers

import (
	"net"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
)

// ReadStamped reads a packet of c into b and returns the time it came: the
// time the kernel got it when stamped, as StampArrivals reported, so that
// a reader that runs late does not make the packet late, or else now on
// clk. The kernel stamps the real time, so only the real clock uses them.
// oob is room for the stamp, RxOOBLen octets.
func ReadStamped(c *net.UDPConn, clk clock.Clock, stamped bool, b, oob []byte) (int, *net.UDPAddr, time.Time, error) {
	if !stamped || !clock.IsReal(clk) {
		n, addr, err := c.ReadFromUDP(b)
		return n, addr, clock.Or(clk).Now(), err //nolint:wrapcheck // the caller looks for net.ErrClosed
	}
	n, oobn, _, addr, err := c.ReadMsgUDP(b, oob)
	at, ok := Arrival(oob[:oobn])
	if !ok {
		at = time.Now()
	}
	return n, addr, at, err //nolint:wrapcheck // the caller looks for net.ErrClosed
}
