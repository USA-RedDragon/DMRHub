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

package ipfilter_test

import (
	"net"
	"net/netip"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/ipfilter"
	"github.com/stretchr/testify/assert"
)

func TestListAllows(t *testing.T) {
	t.Parallel()
	entries := []string{"203.0.113.7", "198.51.100.0/24", "2001:db8::/32", "2001:db8:1::5", "bad entry"}
	block := ipfilter.Compile(config.IPList{Mode: config.IPListBlock, Block: entries, Allow: []string{"192.0.2.1"}})
	allow := ipfilter.Compile(config.IPList{Mode: config.IPListAllow, Allow: entries, Block: []string{"192.0.2.1"}})
	for addr, listed := range map[string]bool{
		"203.0.113.7":          true,
		"203.0.113.8":          false,
		"198.51.100.255":       true,
		"198.51.101.0":         false,
		"::ffff:198.51.100.9":  true,
		"2001:db8:ffff::1":     true,
		"2001:db9::1":          false,
		"::ffff:203.0.113.8":   false,
		"192.0.2.1":            false,
		"fe80::1%eth0":         false,
		"2001:db8:1::5%lo":     true,
		"::203.0.113.7":        false,
		"::ffff:c633:6401":     true,
		"0.0.0.0":              false,
		"::":                   false,
		"2001:0db8:0000::0001": true,
	} {
		a := netip.MustParseAddr(addr)
		assert.Equal(t, !listed, block.Allows(a), "block %s", addr)
		assert.Equal(t, listed, allow.Allows(a), "allow %s", addr)
	}
	assert.True(t, block.Allows(netip.Addr{}), "block mode takes an address it cannot read")
	assert.False(t, allow.Allows(netip.Addr{}), "allow mode refuses an address it cannot read")
}

func TestListOff(t *testing.T) {
	t.Parallel()
	var none *ipfilter.List
	off := ipfilter.Compile(config.IPList{Block: []string{"0.0.0.0/0", "::/0"}})
	unknown := ipfilter.Compile(config.IPList{Mode: "deny", Allow: []string{"192.0.2.1"}})
	for _, l := range []*ipfilter.List{none, off, unknown, ipfilter.Compile(config.IPList{Mode: config.IPListOff})} {
		assert.False(t, l.Active())
		assert.True(t, l.Allows(netip.MustParseAddr("203.0.113.7")))
		assert.True(t, l.Allows(netip.Addr{}))
	}
	everything := ipfilter.Compile(config.IPList{Mode: config.IPListBlock, Block: []string{"0.0.0.0/0", "::/0"}})
	assert.False(t, everything.Allows(netip.MustParseAddr("203.0.113.7")))
	assert.False(t, everything.Allows(netip.MustParseAddr("2001:db8::1")))
}

func TestAddrOf(t *testing.T) {
	t.Parallel()
	want := netip.MustParseAddr("203.0.113.7")
	assert.Equal(t, want, ipfilter.AddrOf(&net.UDPAddr{IP: net.ParseIP("203.0.113.7"), Port: 1}))
	assert.Equal(t, want, ipfilter.AddrOf(&net.UDPAddr{IP: net.ParseIP("::ffff:203.0.113.7"), Port: 1}))
	assert.Equal(t, want, ipfilter.AddrOf(&net.TCPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 1}))
	assert.Equal(t, netip.MustParseAddr("fe80::1"), ipfilter.AddrOf(&net.UDPAddr{IP: net.ParseIP("fe80::1"), Zone: "eth0"}))
	assert.False(t, ipfilter.AddrOf(nil).IsValid())
	assert.False(t, ipfilter.AddrOf((*net.UDPAddr)(nil)).IsValid())
	assert.False(t, ipfilter.AddrOf(&net.UnixAddr{Name: "/run/x", Net: "unix"}).IsValid())
}

func TestListAllowsDoesNotAllocate(t *testing.T) { //nolint:paralleltest // AllocsPerRun counts the allocations of every goroutine
	l := ipfilter.Compile(config.IPList{Mode: config.IPListAllow, Allow: []string{"198.51.100.0/24", "2001:db8::/32", "203.0.113.7"}})
	a := &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 62031}
	allocs := testing.AllocsPerRun(1000, func() {
		_ = l.Allows(ipfilter.AddrOf(a))
	})
	assert.Zero(t, allocs)
}
