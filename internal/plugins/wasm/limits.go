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

// Package wasm runs the plugins inside DMRHub: WebAssembly modules built
// for wasip1, in a sandbox with no files, no environment and no network but
// the hosts that an admin allowed, with limits on memory and CPU. A plugin
// talks to the hub on its stdin and stdout, in the messages of the plugin
// protocol.
package wasm

import (
	"net"
	"time"
)

// Limits are the sandbox of the plugins inside DMRHub.
type Limits struct {
	// MemoryPages is the most memory of a plugin, in pages of 64 KiB.
	MemoryPages uint32
	// CPUShare is the share of one core that a plugin may use, over each
	// CPUWindow. A plugin that uses more is stopped.
	CPUShare  float64
	CPUWindow time.Duration
	// HelloWithin is how long a plugin has to say hello after it starts.
	HelloWithin time.Duration
	// AnswerWithin is how long a plugin may leave what the hub sent it
	// unread. A plugin that waits longer is stopped.
	AnswerWithin time.Duration
	// MaxConns is the most network connections of a plugin at once.
	MaxConns int
	// DialTimeout is how long a connection may take to open.
	DialTimeout time.Duration
	// IdleTimeout ends a read or write of a connection that waits longer.
	IdleTimeout time.Duration
	// AllowAddress says whether a plugin may reach ip, an address of a host
	// that an admin allowed. literal is true when the admin allowed the IP
	// address itself, not a name.
	AllowAddress func(ip net.IP, literal bool) bool
	// LogLines is the most lines of a plugin's stderr that are logged each
	// second.
	LogLines int
}

const (
	defaultMemoryPages = 1024
	defaultCPUShare    = 0.5
	defaultCPUWindow   = 10 * time.Second
	defaultHello       = 10 * time.Second
	defaultAnswer      = 30 * time.Second
	defaultMaxConns    = 4
	defaultDial        = 10 * time.Second
	defaultIdle        = 5 * time.Minute
	defaultLogLines    = 20
)

// DefaultLimits returns the sandbox of the plugins inside DMRHub: 64 MiB of
// memory, half a core over 10 seconds, 30 seconds to read what the hub
// sends, 4 connections, and only public addresses for the hosts that an
// admin allowed by name.
func DefaultLimits() Limits {
	return Limits{
		MemoryPages: defaultMemoryPages, CPUShare: defaultCPUShare, CPUWindow: defaultCPUWindow, HelloWithin: defaultHello,
		AnswerWithin: defaultAnswer,
		MaxConns:     defaultMaxConns, DialTimeout: defaultDial, IdleTimeout: defaultIdle, AllowAddress: PublicAddress, LogLines: defaultLogLines,
	}
}

// sharedAddressSpace is 100.64.0.0/10, the addresses of carrier-grade NAT.
var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)} //nolint:gochecknoglobals // a constant network

// PublicAddress says whether a plugin may reach ip. It never reaches
// loopback, link-local (such as a cloud's metadata service), multicast or
// unspecified addresses. A host that an admin allowed by name must have a
// public address, so that its DNS cannot point a plugin at the hub's own
// network; an IP address that an admin allowed may be private, such as a
// device on the hub's LAN.
func PublicAddress(ip net.IP, literal bool) bool {
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.Equal(net.IPv4bcast) {
		return false
	}
	if literal {
		return true
	}
	return !ip.IsPrivate() && !sharedAddressSpace.Contains(ip)
}
