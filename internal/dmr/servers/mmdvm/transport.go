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

import "net"

// udpTransport is the name of the UDP socket of the server.
const udpTransport = "udp"

// transport carries packets between the server and its repeaters. A UDP
// socket is one; a transport for another protocol gives each peer a
// net.Addr of its own.
type transport interface {
	ReadFrom(p []byte) (int, net.Addr, error)
	WriteTo(p []byte, addr net.Addr) (int, error)
	Close() error
}

// endpoint is a repeater address on a named transport.
type endpoint struct {
	transport string
	addr      net.Addr
}

func udpEndpoint(addr net.UDPAddr) endpoint {
	return endpoint{transport: udpTransport, addr: &addr}
}

// String is the session key of the endpoint: the address for UDP, as
// before there were other transports, and the transport and address
// otherwise.
func (e endpoint) String() string {
	if e.addr == nil {
		return ""
	}
	if e.transport == udpTransport {
		return e.addr.String()
	}
	return e.transport + "/" + e.addr.String()
}

// kvAddr returns the address that the KV record of a repeater at e holds.
// Only a UDP address has a port, and only a repeater at one can be resumed
// by another replica.
func (e endpoint) kvAddr() (string, int) {
	if a, ok := e.addr.(*net.UDPAddr); ok && e.transport == udpTransport {
		return a.IP.String(), a.Port
	}
	return e.String(), 0
}

func (e endpoint) matchesKV(ip string, port int) bool {
	kip, kport := e.kvAddr()
	return kip == ip && kport == port
}
