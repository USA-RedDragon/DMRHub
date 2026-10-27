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

// Package ipfilter decides, from the IP lists of the config, whether
// DMRHub takes the packets of a source address.
package ipfilter

import (
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

// List is an IP list of the config, made ready for lookups. The zero List
// and a nil *List accept every address.
type List struct {
	mode config.IPListMode
	set  set
}

// Compile returns the List of l. It leaves out an entry that does not
// parse, which config validation refuses.
func Compile(l config.IPList) *List {
	out := &List{mode: l.Mode}
	switch l.Mode {
	case config.IPListBlock:
		out.set = makeSet(l.Block)
	case config.IPListAllow:
		out.set = makeSet(l.Allow)
	case config.IPListOff:
	default:
		out.mode = config.IPListOff
	}
	return out
}

// Active reports whether the List can refuse an address.
func (l *List) Active() bool {
	return l != nil && l.mode != config.IPListOff && l.mode != ""
}

// Allows reports whether the List accepts the address a. An IPv4 address
// in IPv6 form is the IPv4 address. In allow mode an invalid address is
// refused.
func (l *List) Allows(a netip.Addr) bool {
	if !l.Active() {
		return true
	}
	a = a.Unmap().WithZone("")
	in := a.IsValid() && l.set.contains(a)
	if l.mode == config.IPListBlock {
		return !in
	}
	return in
}

// AddrOf returns the IP of a *net.UDPAddr, a *net.TCPAddr or a net.Addr
// whose String is host:port, with an IPv4 address in IPv6 form as the IPv4
// address. It returns the invalid address for any other.
func AddrOf(addr net.Addr) netip.Addr {
	var a netip.Addr
	switch v := addr.(type) {
	case *net.UDPAddr:
		if v != nil {
			a, _ = netip.AddrFromSlice(v.IP)
		}
	case *net.TCPAddr:
		if v != nil {
			a, _ = netip.AddrFromSlice(v.IP)
		}
	case nil:
	default:
		if ap, err := netip.ParseAddrPort(addr.String()); err == nil {
			a = ap.Addr()
		}
	}
	return a.Unmap().WithZone("")
}

// set holds ranges by their length, so that a lookup costs one map read
// for each length in use.
type set struct {
	v4, v6 []lengthSet
}

type lengthSet struct {
	bits int
	m    map[netip.Prefix]struct{}
}

func makeSet(entries []string) set {
	var s set
	for _, e := range entries {
		p, err := config.ParseIPListEntry(e)
		if err != nil {
			continue
		}
		if p.Addr().Is4() {
			s.v4 = add(s.v4, p)
		} else {
			s.v6 = add(s.v6, p)
		}
	}
	return s
}

func add(sets []lengthSet, p netip.Prefix) []lengthSet {
	for i := range sets {
		if sets[i].bits == p.Bits() {
			sets[i].m[p] = struct{}{}
			return sets
		}
	}
	return append(sets, lengthSet{bits: p.Bits(), m: map[netip.Prefix]struct{}{p: {}}})
}

func (s set) contains(a netip.Addr) bool {
	sets := s.v6
	if a.Is4() {
		sets = s.v4
	}
	for _, ls := range sets {
		p, err := a.Prefix(ls.bits)
		if err != nil {
			continue
		}
		if _, ok := ls.m[p]; ok {
			return true
		}
	}
	return false
}

// RefusalLog logs the refusals of a server at most once a minute for each
// address, so that a flood from a refused address cannot flood the log.
type RefusalLog struct {
	mu   sync.Mutex
	last map[netip.Addr]time.Time
}

// refusalEvery is how often a RefusalLog logs the refusals of one
// address.
const refusalEvery = time.Minute

// refusalKeep is how many addresses a RefusalLog remembers before it
// forgets the ones it logged longest ago.
const refusalKeep = 4096

// Refused logs, at info level and at most once a minute for a, that
// server refused a packet from a by its IP list.
func (r *RefusalLog) Refused(server string, a netip.Addr) {
	now := time.Now()
	r.mu.Lock()
	if r.last == nil {
		r.last = map[netip.Addr]time.Time{}
	}
	if at, ok := r.last[a]; ok && now.Sub(at) < refusalEvery {
		r.mu.Unlock()
		return
	}
	if len(r.last) >= refusalKeep {
		for k, at := range r.last {
			if now.Sub(at) >= refusalEvery {
				delete(r.last, k)
			}
		}
		if len(r.last) >= refusalKeep {
			clear(r.last)
		}
	}
	r.last[a] = now
	r.mu.Unlock()
	slog.Info("Refusing a source address by the IP list", "server", server, "address", a.String())
}
