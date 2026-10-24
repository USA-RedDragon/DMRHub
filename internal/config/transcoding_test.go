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
	"os"
	"path/filepath"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/goccy/go-yaml"
)

func TestTranscodingIsOffByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DMR.Transcoding.AMBEServers) != 0 {
		t.Errorf("the default AMBE servers are %v, want none", cfg.DMR.Transcoding.AMBEServers)
	}
}

func TestTranscodingValidate(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		servers []config.AMBEServer
		err     error
	}{
		{"none", nil, nil},
		{"one", []config.AMBEServer{{Address: "ambeserver:2460"}}, nil},
		{"several", []config.AMBEServer{{Address: "192.0.2.10:2460", Calls: 1}, {Address: "[2001:db8::1]:2460", Calls: 4}}, nil},
		{"no port", []config.AMBEServer{{Address: "ambeserver"}}, config.ErrInvalidAMBEServer},
		{"no host", []config.AMBEServer{{Address: ":2460"}}, config.ErrInvalidAMBEServer},
		{"port 0", []config.AMBEServer{{Address: "ambeserver:0"}}, config.ErrInvalidAMBEServer},
		{"port too high", []config.AMBEServer{{Address: "ambeserver:65536"}}, config.ErrInvalidAMBEServer},
		{"named port", []config.AMBEServer{{Address: "ambeserver:port"}}, config.ErrInvalidAMBEServer},
		{"empty", []config.AMBEServer{{Address: "ambeserver:2460"}, {}}, config.ErrInvalidAMBEServer},
		{"negative calls", []config.AMBEServer{{Address: "ambeserver:2460", Calls: -1}}, config.ErrInvalidAMBECalls},
	} {
		errs := config.Transcoding{AMBEServers: tt.servers}.ValidateWithFields()
		if tt.err == nil && len(errs) > 0 {
			t.Errorf("%s: unexpected errors %v", tt.name, errs)
		}
		if tt.err != nil && (len(errs) != 1 || errs[0].Field != "dmr.transcoding.ambe-servers" || !errors.Is(errs[0].Err, tt.err)) {
			t.Errorf("%s: errors %v, want one %v on dmr.transcoding.ambe-servers", tt.name, errs, tt.err)
		}
	}
}

func TestConfigChecksTheAMBEServers(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DMR.Transcoding.AMBEServers = []config.AMBEServer{{Address: "ambeserver"}}
	found := false
	for _, e := range cfg.ValidateWithFields() {
		found = found || e.Field == "dmr.transcoding.ambe-servers"
	}
	if !found {
		t.Error("the config does not check the AMBE servers")
	}
}

// TestTranscodingFromAFile reads the AMBE servers from a config file, as
// a Setup save writes them.
func TestTranscodingFromAFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("secret: a-secret-of-some-length\npassword-salt: a-salt-of-some-length\nhttp:\n  canonical-host: http://localhost:3005\ndmr:\n  transcoding:\n    ambe-servers:\n      - address: ambeserver:2460\n      - address: thumbdv:2460\n        calls: 1\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configulator.New(config.ConfigSchema()).WithFile(&configulator.FileOptions{Search: []string{path}, Decoders: configulator.Decoders{".yaml": yaml.Unmarshal}}).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []config.AMBEServer{{Address: "ambeserver:2460"}, {Address: "thumbdv:2460", Calls: 1}}
	got := cfg.DMR.Transcoding.AMBEServers
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("AMBE servers %v, want %v", got, want)
	}
}
