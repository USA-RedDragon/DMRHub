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
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ListenUDP opens a UDP socket on bind and port. The bind may be an IP
// address, an IPv6 address in brackets such as "[::]", a host name, or
// empty for every address. It returns the socket and its address: the IP
// of the bind, or the IP of the socket when the bind is empty, and the
// port that the socket holds.
func ListenUDP(bind string, port int) (*net.UDPConn, net.UDPAddr, error) {
	host := strings.TrimSuffix(strings.TrimPrefix(bind, "["), "]")
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, net.UDPAddr{}, fmt.Errorf("error resolving bind address %q: %w", bind, err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, net.UDPAddr{}, fmt.Errorf("error opening UDP socket: %w", err)
	}
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = conn.Close()
		return nil, net.UDPAddr{}, fmt.Errorf("error reading UDP socket address: unexpected %T", conn.LocalAddr())
	}
	socket := *addr
	socket.Port = local.Port
	if socket.IP == nil {
		socket.IP = local.IP
	}
	return conn, socket, nil
}
