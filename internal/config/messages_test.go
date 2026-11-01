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

func TestStoreAndForwardDefaultsHoldSevenDaysAndFiftyTexts(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DMR.Messages != (config.Messages{HoldDays: 7, HoldMax: 50}) {
		t.Errorf("Expected 7 days and 50 texts, got %+v", cfg.DMR.Messages)
	}
	if got := (config.Messages{}).HoldFor(); got != 7*24*time.Hour {
		t.Errorf("Expected 0 days to hold for 7 days, got %v", got)
	}
	if got := (config.Messages{}).MostHeld(); got != 50 {
		t.Errorf("Expected 0 texts to keep 50, got %d", got)
	}
}

func TestStoreAndForwardLimitsAreChecked(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		m    config.Messages
		want error
	}{
		{m: config.Messages{HoldDays: 365, HoldMax: 1000}},
		{m: config.Messages{HoldDays: -1}, want: config.ErrInvalidHoldDays},
		{m: config.Messages{HoldDays: 366}, want: config.ErrInvalidHoldDays},
		{m: config.Messages{HoldMax: -1}, want: config.ErrInvalidHoldMax},
		{m: config.Messages{HoldMax: 1001}, want: config.ErrInvalidHoldMax},
	} {
		err := tc.m.Validate()
		if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("%+v: got %v, want %v", tc.m, err, tc.want)
		}
	}
}
