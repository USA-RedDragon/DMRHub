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

package testutils

import (
	"encoding/binary"
	"errors"
	"net"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"golang.org/x/sys/unix"
)

const stampOOBLen = 128

// stampTx asks the kernel to stamp when each packet that c sends leaves,
// and reports whether it does.
func stampTx(c *net.UDPConn) bool {
	return setTimestamping(c, unix.SOF_TIMESTAMPING_TX_SOFTWARE|unix.SOF_TIMESTAMPING_SOFTWARE|unix.SOF_TIMESTAMPING_OPT_TSONLY)
}

func setTimestamping(c *net.UDPConn, flags int) bool {
	raw, err := c.SyscallConn()
	if err != nil {
		return false
	}
	var serr error
	if err := raw.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TIMESTAMPING, flags)
	}); err != nil {
		return false
	}
	return serr == nil
}

// sentAt writes b to addr from c, which stampTx set up, and returns when
// the kernel sent it, or false when it did not tell. It then stops the
// stamps of c.
func sentAt(c *net.UDPConn, b []byte, addr *net.UDPAddr) (time.Time, bool) {
	defer setTimestamping(c, 0)
	if _, err := c.WriteToUDP(b, addr); err != nil {
		return time.Time{}, false
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return time.Time{}, false
	}
	var at time.Time
	ok := false
	oob := make([]byte, stampOOBLen)
	_ = raw.Control(func(fd uintptr) {
		for {
			_, oobn, _, _, err := unix.Recvmsg(int(fd), nil, oob, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
			if err != nil {
				if errors.Is(err, unix.EINTR) {
					continue
				}
				return
			}
			if t, found := txStamp(oob[:oobn]); found {
				at, ok = t, true
				return
			}
		}
	})
	return at, ok
}

func txStamp(oob []byte) (time.Time, bool) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return time.Time{}, false
	}
	for _, m := range msgs {
		if m.Header.Level != unix.SOL_SOCKET || m.Header.Type != unix.SO_TIMESTAMPING {
			continue
		}
		switch len(m.Data) {
		case 48:
			sec := int64(binary.NativeEndian.Uint64(m.Data[:8]))    //nolint:gosec // a time_t
			nsec := int64(binary.NativeEndian.Uint64(m.Data[8:16])) //nolint:gosec // a time_t
			return time.Unix(sec, nsec), true
		case 24:
			sec := int64(int32(binary.NativeEndian.Uint32(m.Data[:4])))   //nolint:gosec // a 32-bit time_t
			nsec := int64(int32(binary.NativeEndian.Uint32(m.Data[4:8]))) //nolint:gosec // a 32-bit time_t
			return time.Unix(sec, nsec), true
		}
	}
	return time.Time{}, false
}

// stampRx asks the kernel to stamp when each packet that c receives came.
func stampRx(c *net.UDPConn) {
	servers.StampArrivals(c)
}

// readStamped reads a packet from c, which stampRx set up, and returns
// when it came, or false when the kernel did not tell.
func readStamped(c *net.UDPConn, b, oob []byte) (int, *net.UDPAddr, time.Time, bool, error) {
	n, oobn, _, from, err := c.ReadMsgUDP(b, oob)
	if err != nil {
		return 0, nil, time.Time{}, false, err //nolint:wrapcheck // the caller looks for net.ErrClosed
	}
	at, ok := servers.Arrival(oob[:oobn])
	return n, from, at, ok, nil
}
