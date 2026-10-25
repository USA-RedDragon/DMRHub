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

func TestDrainTimeout(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DMR.DrainTimeout != 25 {
		t.Errorf("the default drain timeout is %d, want 25", cfg.DMR.DrainTimeout)
	}
	if err := cfg.DMR.Validate(); err != nil {
		t.Errorf("the defaults do not validate: %v", err)
	}
	cfg.DMR.DrainTimeout = 0
	if err := cfg.DMR.Validate(); err != nil {
		t.Errorf("0 stops at once, got %v", err)
	}
	cfg.DMR.DrainTimeout = -1
	if err := cfg.DMR.Validate(); !errors.Is(err, config.ErrInvalidDrainTimeout) {
		t.Errorf("a negative drain timeout gave %v", err)
	}
}
