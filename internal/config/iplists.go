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

package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// IPLists holds the source addresses that DMRHub refuses or alone
// accepts, apart for the repeaters and for the OpenBridge peers.
type IPLists struct {
	Repeaters IPList `json:"repeaters" name:"repeaters" description:"Addresses of MMDVM hotspots and repeaters, browser radios over WebTransport and IPSC repeaters"`
	Peers     IPList `json:"peers" name:"peers" description:"Addresses of OpenBridge peers"`
}

// IPList is a block list and an allow list of addresses, and which of
// them applies.
type IPList struct {
	Mode  IPListMode `json:"mode" name:"mode" description:"Which list applies. One of off (accept every address), block (refuse the addresses of the block list) or allow (refuse every address but those of the allow list)" default:"off"`
	Block []string   `json:"block" yaml:"block,omitempty" name:"block" description:"Addresses and CIDR ranges, IPv4 or IPv6, such as 203.0.113.7 or 2001:db8::/32, that the block mode refuses"`
	Allow []string   `json:"allow" yaml:"allow,omitempty" name:"allow" description:"Addresses and CIDR ranges, IPv4 or IPv6, that the allow mode accepts"`
}

// IPListMode is which list of an IPList applies.
type IPListMode string

const (
	// IPListOff accepts every address.
	IPListOff IPListMode = "off"
	// IPListBlock refuses the addresses of the block list.
	IPListBlock IPListMode = "block"
	// IPListAllow refuses every address but those of the allow list.
	IPListAllow IPListMode = "allow"
)

var (
	// ErrInvalidIPListMode is an IP list mode that is not off, block or allow.
	ErrInvalidIPListMode = errors.New("the mode must be off, block or allow")
	// ErrInvalidIPListEntry is an entry of an IP list that is not an IP
	// address or a CIDR range.
	ErrInvalidIPListEntry = errors.New("not an IP address or CIDR range, such as 203.0.113.7, 203.0.113.0/24 or 2001:db8::/32")
	// ErrIPListHostBits is a CIDR range with bits set after its prefix.
	ErrIPListHostBits = errors.New("the range has bits set after its prefix")
)

// ParseIPListEntry returns the range of an entry of an IP list: an IPv4 or
// IPv6 address, which is a range of one address, or a CIDR range. An IPv4
// address in IPv6 form, such as ::ffff:203.0.113.7, is the IPv4 address.
func ParseIPListEntry(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		a, err := netip.ParseAddr(s)
		if err != nil || a.Zone() != "" {
			return netip.Prefix{}, fmt.Errorf("%q: %w", s, ErrInvalidIPListEntry)
		}
		a = a.Unmap()
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q: %w", s, ErrInvalidIPListEntry)
	}
	if p.Masked() != p {
		return netip.Prefix{}, fmt.Errorf("%q: %w; use %s", s, ErrIPListHostBits, p.Masked())
	}
	if a := p.Addr(); a.Is4In6() {
		p = netip.PrefixFrom(a.Unmap(), p.Bits()-96)
	}
	return p, nil
}

// Validate validates the IP lists.
func (l IPLists) Validate() error {
	return firstValidationError(l.ValidateWithFields())
}

func (l IPLists) ValidateWithFields() (errs []ValidationError) {
	errs = append(errs, l.Repeaters.validate("dmr.ip-lists.repeaters")...)
	return append(errs, l.Peers.validate("dmr.ip-lists.peers")...)
}

func (l IPList) validate(field string) (errs []ValidationError) {
	switch l.Mode {
	case "", IPListOff, IPListBlock, IPListAllow:
	default:
		errs = append(errs, ValidationError{Field: field + ".mode", Err: ErrInvalidIPListMode})
	}
	errs = append(errs, validateEntries(field+".block", l.Block)...)
	return append(errs, validateEntries(field+".allow", l.Allow)...)
}

func validateEntries(field string, entries []string) []ValidationError {
	for _, e := range entries {
		if _, err := ParseIPListEntry(e); err != nil {
			return []ValidationError{{Field: field, Err: err}}
		}
	}
	return nil
}
