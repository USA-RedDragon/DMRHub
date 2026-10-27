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
	"errors"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/configulator/v2"
)

func TestPublicAddressDefault(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DMR.PublicAddress != "" {
		t.Errorf("the default public address is %q, want empty", cfg.DMR.PublicAddress)
	}
	if err := cfg.DMR.Validate(); err != nil {
		t.Errorf("the defaults do not validate: %v", err)
	}
}

func TestPublicAddressValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		addr string
		ok   bool
	}{
		{"", true},
		{"dmr.example.org", true},
		{"hub-1.example.org", true},
		{"localhost", true},
		{"192.0.2.10", true},
		{"2001:db8::10", true},
		{"https://dmr.example.org", false},
		{"dmr.example.org:62031", false},
		{"dmr.example.org/enroll", false},
		{"dmr example.org", false},
		{"-dmr.example.org", false},
		{"dmr..example.org", false},
		{"[2001:db8::10]", false},
	}
	for _, tc := range cases {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		if err != nil {
			t.Fatal(err)
		}
		cfg.DMR.PublicAddress = tc.addr
		err = cfg.DMR.Validate()
		if tc.ok && err != nil {
			t.Errorf("%q gave %v", tc.addr, err)
		}
		if !tc.ok && !errors.Is(err, config.ErrInvalidDMRPublicAddress) {
			t.Errorf("%q gave %v, want ErrInvalidDMRPublicAddress", tc.addr, err)
		}
	}
}

func TestServerAddress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		public, canonical, want string
	}{
		{"dmr.example.org", "https://hub.example.org", "dmr.example.org"},
		{"", "https://hub.example.org", "hub.example.org"},
		{"", "https://hub.example.org:8443/dmrhub", "hub.example.org"},
		{"", "hub.example.org", "hub.example.org"},
		{"", "http://[2001:db8::1]:3005", "2001:db8::1"},
		{"", "", ""},
	}
	for _, tc := range cases {
		var cfg config.Config
		cfg.DMR.PublicAddress = tc.public
		cfg.HTTP.CanonicalHost = tc.canonical
		if got := cfg.ServerAddress(); got != tc.want {
			t.Errorf("public %q, canonical host %q: got %q, want %q", tc.public, tc.canonical, got, tc.want)
		}
	}
}
