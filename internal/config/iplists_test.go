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

package config_test

import (
	"net/netip"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIPListEntry(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"203.0.113.7":            "203.0.113.7/32",
		"2001:db8::1":            "2001:db8::1/128",
		"2001:db8::/32":          "2001:db8::/32",
		"::ffff:203.0.113.7":     "203.0.113.7/32",
		"::ffff:203.0.113.0/120": "203.0.113.0/24",
		"0.0.0.0/0":              "0.0.0.0/0",
		"::/0":                   "::/0",
	} {
		p, err := config.ParseIPListEntry(in)
		require.NoError(t, err, in)
		assert.Equal(t, netip.MustParsePrefix(want), p, in)
	}
	p, err := config.ParseIPListEntry("\t203.0.113.0/24 ")
	require.NoError(t, err)
	assert.Equal(t, netip.MustParsePrefix("203.0.113.0/24"), p)
	for _, in := range []string{"", "example.org", "203.0.113", "203.0.113.0/33", "fe80::1%eth0"} {
		_, err := config.ParseIPListEntry(in)
		require.ErrorIs(t, err, config.ErrInvalidIPListEntry, in)
	}
	_, err = config.ParseIPListEntry("10.1.2.3/8")
	require.ErrorIs(t, err, config.ErrIPListHostBits)
	assert.Contains(t, err.Error(), "10.0.0.0/8")
	_, err = config.ParseIPListEntry("::ffff:0:0/90")
	require.ErrorIs(t, err, config.ErrIPListHostBits)
}

func TestIPListsValidate(t *testing.T) {
	t.Parallel()
	ok := config.IPLists{
		MMDVM:        config.IPList{Mode: config.IPListBlock, Block: []string{"203.0.113.0/24", "2001:db8::/32"}},
		WebTransport: config.IPList{Mode: config.IPListAllow, Allow: []string{"2001:db8::1"}},
		IPSC:         config.IPList{Mode: config.IPListOff, Block: []string{"192.0.2.1"}},
		OpenBridge:   config.IPList{Mode: config.IPListAllow, Allow: []string{"198.51.100.7"}},
	}
	require.Empty(t, ok.ValidateWithFields())
	require.Empty(t, config.IPLists{}.ValidateWithFields())

	bad := config.IPLists{
		MMDVM:        config.IPList{Mode: "deny", Allow: []string{"nope"}},
		WebTransport: config.IPList{Allow: []string{"::1/129"}},
		IPSC:         config.IPList{Mode: config.IPListBlock, Block: []string{"x"}},
		OpenBridge:   config.IPList{Block: []string{"10.1.2.3/8"}},
	}
	errs := bad.ValidateWithFields()
	fields := make([]string, 0, len(errs))
	for _, e := range errs {
		fields = append(fields, e.Field)
	}
	assert.Equal(t, []string{"dmr.ip-lists.mmdvm.mode", "dmr.ip-lists.mmdvm.allow", "dmr.ip-lists.webtransport.allow", "dmr.ip-lists.ipsc.block", "dmr.ip-lists.openbridge.block"}, fields)
	require.ErrorIs(t, errs[0].Err, config.ErrInvalidIPListMode)
}
