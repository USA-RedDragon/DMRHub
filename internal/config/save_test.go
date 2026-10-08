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
	"github.com/google/go-cmp/cmp"
)

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	want := makeValidConfig()
	want.Redis = config.Redis{Enabled: true, Host: "redis", Port: 6380, Password: "redispass"}
	want.Database.ExtraParameters = []string{}
	want.HTTP.TrustedProxies = []string{"10.0.0.0/8"}
	want.HTTP.CORS = config.CORS{Enabled: true, Hosts: []string{"https://example.com"}}
	want.DMR.IPSC = config.IPSC{Enabled: true, Bind: "[::]", Port: 50000, NetworkID: 1234}
	want.DMR.DisableRadioIDValidation = true
	want.SMTP = config.SMTP{
		Enabled:    true,
		Host:       "smtp.example.com",
		Port:       465,
		TLS:        config.SMTPTLSImplicit,
		Username:   "user",
		Password:   "smtppass",
		From:       "dmrhub@example.com",
		AuthMethod: config.SMTPAuthMethodPlain,
	}
	want.NetworkName = "Test Network"
	want.HIBPAPIKey = "hibp"

	if err := want.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() error: %v", err)
	}

	got, err := configulator.New(config.ConfigSchema()).
		WithFile(&configulator.FileOptions{
			Search:       []string{filepath.Join(configDir, "DMRHub", "config.yaml")},
			RequireFound: true,
			Decoders:     configulator.Decoders{".yaml": yaml.Unmarshal},
		}).
		LoadWithoutValidation()
	if err != nil {
		t.Fatalf("loading saved config: %v", err)
	}

	defaults, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatalf("Default() error: %v", err)
	}
	want.Metrics = defaults.Metrics
	want.PProf = defaults.PProf
	want.DMR.OpenBridge = defaults.DMR.OpenBridge

	if diff := cmp.Diff(want, *got); diff != "" {
		t.Errorf("saved config did not round-trip (-want +got):\n%s", diff)
	}
}
