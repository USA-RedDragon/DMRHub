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

func TestRetentionDefaultsKeepForever(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DMR.Retention != (config.Retention{}) {
		t.Errorf("Expected every retention to default to 0, got %+v", cfg.DMR.Retention)
	}
}

func TestRetentionValidate(t *testing.T) {
	t.Parallel()
	for _, days := range []int{0, 30, 90, 365, 730, 1825, config.MaxRetentionDays} {
		r := config.Retention{Messages: days, Transmissions: days, Bursts: days}
		if err := r.Validate(); err != nil {
			t.Errorf("Expected %d days to be valid, got %v", days, err)
		}
	}
	for _, days := range []int{-1, config.MaxRetentionDays + 1} {
		cfg := makeValidConfig()
		cfg.DMR.Retention = config.Retention{Messages: days, Transmissions: days, Bursts: days}
		fields := map[string]bool{}
		for _, e := range cfg.ValidateWithFields() {
			if errors.Is(e.Err, config.ErrInvalidRetention) {
				fields[e.Field] = true
			}
		}
		for _, f := range []string{"dmr.retention.messages", "dmr.retention.transmissions", "dmr.retention.bursts"} {
			if !fields[f] {
				t.Errorf("Expected %s to be invalid for %d days", f, days)
			}
		}
	}
}
