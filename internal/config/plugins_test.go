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

func TestPluginsDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.DMR.Plugins
	if p.Enabled || p.Port != 3006 || p.Bind != "[::]" || p.CertFile != "" || p.KeyFile != "" {
		t.Errorf("unexpected plugin defaults %+v", p)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("the defaults do not validate: %v", err)
	}
}

func TestPluginsValidate(t *testing.T) {
	t.Parallel()
	valid := config.Plugins{Enabled: true, Bind: "[::]", Port: 3006, CertFile: "/tls/tls.crt", KeyFile: "/tls/tls.key"}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
	off := config.Plugins{Port: -1, CertFile: "/tls/tls.crt"}
	if err := off.Validate(); err != nil {
		t.Errorf("a listener that is off is not checked, got %v", err)
	}
	for _, tt := range []struct {
		name string
		edit func(*config.Plugins)
		err  error
	}{
		{"port 0", func(p *config.Plugins) { p.Port = 0 }, config.ErrInvalidPluginsPort},
		{"port 65536", func(p *config.Plugins) { p.Port = 65536 }, config.ErrInvalidPluginsPort},
		{"no bind", func(p *config.Plugins) { p.Bind = "" }, config.ErrInvalidPluginsBind},
		{"cert alone", func(p *config.Plugins) { p.KeyFile = "" }, config.ErrPluginsKeyPair},
		{"key alone", func(p *config.Plugins) { p.CertFile = "" }, config.ErrPluginsKeyPair},
	} {
		p := valid
		tt.edit(&p)
		if err := p.Validate(); !errors.Is(err, tt.err) {
			t.Errorf("%s: got %v, want %v", tt.name, err, tt.err)
		}
	}
}

func TestPluginsPortMayNotBeTheHTTPPort(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DMR.Plugins.Enabled = true
	cfg.DMR.Plugins.Port = cfg.HTTP.Port
	found := false
	for _, e := range cfg.ValidateWithFields() {
		if e.Field == "dmr.plugins.port" && errors.Is(e.Err, config.ErrInvalidPluginsPort) {
			found = true
		}
	}
	if !found {
		t.Error("the plugin port may not be the HTTP port")
	}
}
