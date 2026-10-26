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

func modemDefaults(t *testing.T) config.DMR {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.DMR
}

func TestModemDefaults(t *testing.T) {
	t.Parallel()
	d := modemDefaults(t)
	m := d.Modem
	if m.Enabled || m.Duplex || m.Baud != 115200 || m.ColorCode != 1 || m.RFLevel != 100 || m.RXLevel != 50 ||
		m.TXLevel != 50 || !m.TXInvert || m.RXInvert || m.PTTInvert || m.TXDelay != 100 || m.DMRDelay != 0 {
		t.Fatalf("defaults: %+v", m)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("the defaults do not validate: %v", err)
	}
}

func TestModemValidation(t *testing.T) {
	t.Parallel()
	good := func(m *config.Modem) {
		m.Enabled = true
		m.Port = "/dev/ttyAMA0"
		m.RepeaterID = 311860199
		m.RXFrequency = 438800000
		m.TXFrequency = 438800000
	}
	for _, c := range []struct {
		name  string
		edit  func(m *config.Modem)
		field string
		err   error
	}{
		{"no port", func(m *config.Modem) { m.Port = "" }, "dmr.modem.port", config.ErrInvalidModemPort},
		{"baud", func(m *config.Modem) { m.Baud = 1200 }, "dmr.modem.baud", config.ErrInvalidModemBaud},
		{"no repeater", func(m *config.Modem) { m.RepeaterID = 0 }, "dmr.modem.repeater-id", config.ErrInvalidModemRepeaterID},
		{"repeater too big", func(m *config.Modem) { m.RepeaterID = 1 << 32 }, "dmr.modem.repeater-id", config.ErrInvalidModemRepeaterID},
		{"color code", func(m *config.Modem) { m.ColorCode = 16 }, "dmr.modem.color-code", config.ErrInvalidModemColorCode},
		{"no rx frequency", func(m *config.Modem) { m.RXFrequency = 0 }, "dmr.modem.rx-frequency", config.ErrInvalidModemFrequency},
		{"no tx frequency", func(m *config.Modem) { m.TXFrequency = 0 }, "dmr.modem.tx-frequency", config.ErrInvalidModemFrequency},
		{"tx frequency above 32 bits", func(m *config.Modem) { m.TXFrequency = 1 << 32 }, "dmr.modem.tx-frequency", config.ErrInvalidModemFrequency},
		{"rx offset below 0 Hz", func(m *config.Modem) { m.RXOffset = -438800001 }, "dmr.modem.rx-frequency", config.ErrInvalidModemFrequency},
		{"rf level", func(m *config.Modem) { m.RFLevel = 101 }, "dmr.modem.rf-level", config.ErrInvalidModemLevel},
		{"rx level", func(m *config.Modem) { m.RXLevel = -1 }, "dmr.modem.rx-level", config.ErrInvalidModemLevel},
		{"tx level", func(m *config.Modem) { m.TXLevel = 100.5 }, "dmr.modem.tx-level", config.ErrInvalidModemLevel},
		{"rx dc offset", func(m *config.Modem) { m.RXDCOffset = 128 }, "dmr.modem.rx-dc-offset", config.ErrInvalidModemDCOffset},
		{"tx dc offset", func(m *config.Modem) { m.TXDCOffset = -129 }, "dmr.modem.tx-dc-offset", config.ErrInvalidModemDCOffset},
		{"tx delay", func(m *config.Modem) { m.TXDelay = 510 }, "dmr.modem.tx-delay", config.ErrInvalidModemTXDelay},
		{"dmr delay", func(m *config.Modem) { m.DMRDelay = 256 }, "dmr.modem.dmr-delay", config.ErrInvalidModemDMRDelay},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := modemDefaults(t)
			good(&d.Modem)
			if err := d.Validate(); err != nil {
				t.Fatalf("the good modem does not validate: %v", err)
			}
			c.edit(&d.Modem)
			errs := d.ValidateWithFields()
			if len(errs) != 1 || errs[0].Field != c.field || !errors.Is(errs[0].Err, c.err) {
				t.Fatalf("got %v, want %s: %v", errs, c.field, c.err)
			}
			d.Modem.Enabled = false
			if err := d.Validate(); err != nil {
				t.Fatalf("a disabled modem is not checked: %v", err)
			}
		})
	}
}
