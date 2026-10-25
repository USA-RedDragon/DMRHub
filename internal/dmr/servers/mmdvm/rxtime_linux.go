//go:build linux

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

package mmdvm

import (
	"encoding/binary"
	"net"
	"syscall"
	"time"
)

// rxOOBLen is the room for the control message of a receive time.
const rxOOBLen = 64

// stampArrivals asks the kernel to stamp each packet that c gets with the
// time it came, and reports whether it does.
func stampArrivals(c *net.UDPConn) bool {
	raw, err := c.SyscallConn()
	if err != nil {
		return false
	}
	var serr error
	if err := raw.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_TIMESTAMPNS, 1)
	}); err != nil {
		return false
	}
	return serr == nil
}

// arrival returns the time in the control messages oob at which the
// kernel got a packet.
func arrival(oob []byte) (time.Time, bool) {
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return time.Time{}, false
	}
	for _, m := range msgs {
		if m.Header.Level != syscall.SOL_SOCKET || m.Header.Type != syscall.SCM_TIMESTAMPNS {
			continue
		}
		switch len(m.Data) {
		case 16:
			sec := int64(binary.NativeEndian.Uint64(m.Data[:8]))  //nolint:gosec // a time_t
			nsec := int64(binary.NativeEndian.Uint64(m.Data[8:])) //nolint:gosec // a time_t
			return time.Unix(sec, nsec), true
		case 8:
			sec := int64(int32(binary.NativeEndian.Uint32(m.Data[:4])))  //nolint:gosec // a 32-bit time_t
			nsec := int64(int32(binary.NativeEndian.Uint32(m.Data[4:]))) //nolint:gosec // a 32-bit time_t
			return time.Unix(sec, nsec), true
		}
	}
	return time.Time{}, false
}
