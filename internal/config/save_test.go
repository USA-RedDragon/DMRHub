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
	"os"
	"path/filepath"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/goccy/go-yaml"
)

func TestOpenBridgeServerIDLoadsFromTheFile(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("dmr:\n  openbridge:\n    server-id: 3129\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := configulator.New(config.ConfigSchema()).
		WithFile(&configulator.FileOptions{
			Search:       []string{file},
			RequireFound: true,
			Decoders:     configulator.Decoders{".yaml": yaml.Unmarshal},
		}).
		LoadWithoutValidation()
	if err != nil {
		t.Fatalf("loading the config: %v", err)
	}
	if got.DMR.OpenBridge.ServerID != 3129 {
		t.Fatalf("dmr.openbridge.server-id %d, want 3129", got.DMR.OpenBridge.ServerID)
	}
}
