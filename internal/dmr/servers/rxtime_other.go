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

package servers

import (
	"net"
	"time"
)

// RxOOBLen is the room for the control message of a receive time.
const RxOOBLen = 0

// StampArrivals reports false, as only Linux stamps the packets.
func StampArrivals(*net.UDPConn) bool { return false }

// Arrival reports no time, as only Linux stamps the packets.
func Arrival([]byte) (time.Time, bool) { return time.Time{}, false }
