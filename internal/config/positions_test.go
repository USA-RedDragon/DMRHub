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

func TestPositionDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.DMR.Positions
	if p.MinInterval != 5 || p.MaxRetention != 30 || p.APRSIS.Callsign != "" || p.APRSIS.Server != "rotate.aprs2.net:14580" {
		t.Errorf("unexpected position defaults %+v", p)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("the defaults do not validate: %v", err)
	}
}

func TestPositionValidate(t *testing.T) {
	t.Parallel()
	valid := config.Positions{MinInterval: 10, MaxRetention: 365, APRSIS: config.APRSIS{Callsign: "N0HUB-10", Passcode: "12345", Server: "rotate.aprs2.net:14580"}}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
	for _, tt := range []struct {
		name string
		edit func(*config.Positions)
		err  error
	}{
		{"interval 7", func(p *config.Positions) { p.MinInterval = 7 }, config.ErrInvalidPositionInterval},
		{"retention -1", func(p *config.Positions) { p.MaxRetention = -1 }, config.ErrInvalidPositionRetention},
		{"retention 366", func(p *config.Positions) { p.MaxRetention = 366 }, config.ErrInvalidPositionRetention},
		{"lower case callsign", func(p *config.Positions) { p.APRSIS.Callsign = "n0hub" }, config.ErrInvalidAPRSCallsign},
		{"injected callsign", func(p *config.Positions) { p.APRSIS.Callsign = "N0HUB>APRS" }, config.ErrInvalidAPRSCallsign},
		{"passcode", func(p *config.Positions) { p.APRSIS.Passcode = "abc" }, config.ErrInvalidAPRSPasscode},
		{"server", func(p *config.Positions) { p.APRSIS.Server = "rotate.aprs2.net" }, config.ErrInvalidAPRSServer},
	} {
		p := valid
		tt.edit(&p)
		if err := p.Validate(); !errors.Is(err, tt.err) {
			t.Errorf("%s: expected %v, got %v", tt.name, tt.err, err)
		}
	}
}

func TestPositionZeroValuesMeanTheDefaults(t *testing.T) {
	t.Parallel()
	var p config.Positions
	if err := p.Validate(); err != nil {
		t.Errorf("a config without a positions section must validate: %v", err)
	}
	if p.Interval() != 5 || p.Retention() != 30 || p.APRSIS.ServerAddress() != "rotate.aprs2.net:14580" {
		t.Errorf("unexpected defaults %d %d %s", p.Interval(), p.Retention(), p.APRSIS.ServerAddress())
	}
}
