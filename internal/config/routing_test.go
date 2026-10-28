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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/configulator/v2"
)

func TestRoutingDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Routing{DynamicTimeout: 15 * time.Minute, Transmit: config.TransmitAny, Contention: config.ContentionMix}
	if cfg.DMR.Routing != want {
		t.Errorf("routing defaults %+v, want %+v", cfg.DMR.Routing, want)
	}
	if cfg.DMR.RepeaterQuota != 100 {
		t.Errorf("repeater quota %d, want 100", cfg.DMR.RepeaterQuota)
	}
}

func TestRoutingValidate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		field string
		r     config.Routing
		want  error
	}{
		{"dmr.routing.dynamic-timeout", config.Routing{DynamicTimeout: -time.Second}, config.ErrInvalidDynamicTimeout},
		{"dmr.routing.contention", config.Routing{Contention: "loudest"}, config.ErrInvalidContention},
		{"dmr.routing.transmit", config.Routing{Transmit: "friends"}, config.ErrInvalidTransmit},
		{"dmr.routing.max-transmission", config.Routing{MaxTransmission: -time.Second}, config.ErrInvalidMaxTransmission},
	} {
		cfg := makeValidConfig()
		cfg.DMR.Routing = c.r
		var got error
		for _, e := range cfg.ValidateWithFields() {
			if e.Field == c.field {
				got = e.Err
			}
		}
		if !errors.Is(got, c.want) {
			t.Errorf("%s: error %v, want %v", c.field, got, c.want)
		}
	}
	quota := makeValidConfig()
	quota.DMR.RepeaterQuota = -1
	if err := quota.DMR.Validate(); !errors.Is(err, config.ErrInvalidRepeaterQuota) {
		t.Errorf("a negative repeater quota: error %v, want %v", err, config.ErrInvalidRepeaterQuota)
	}
	cfg := makeValidConfig()
	cfg.DMR.Routing = config.Routing{DynamicTimeout: time.Hour, Transmit: config.TransmitMembers, MaxTransmission: 3 * time.Minute, Contention: config.ContentionFirst}
	if err := cfg.DMR.Routing.Validate(); err != nil {
		t.Errorf("valid routing settings: %v", err)
	}
}
