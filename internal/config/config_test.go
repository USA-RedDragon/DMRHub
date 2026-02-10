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
)

func makeValidConfig() config.Config {
	return config.Config{
		LogLevel:     config.LogLevelInfo,
		Secret:       "testsecret",
		PasswordSalt: "testsalt",
		HTTP: config.HTTP{
			Bind:          "[::]",
			Port:          3005,
			CanonicalHost: "http://localhost:3005",
			RobotsTXT: config.RobotsTXT{
				Mode: config.RobotsTXTModeDisabled,
			},
		},
		DMR: config.DMR{
			MMDVM: config.MMDVM{
				Bind: "[::]",
				Port: 62031,
			},
		},
		Database: config.Database{
			Driver:   config.DatabaseDriverSQLite,
			Database: "test.db",
		},
	}
}

// --- Redis Validation ---

func TestRedisValidateDisabled(t *testing.T) {
	t.Parallel()
	r := config.Redis{Enabled: false}
	if err := r.Validate(); err != nil {
		t.Errorf("Expected nil error for disabled Redis, got %v", err)
	}
}

func TestRedisValidateEmptyHost(t *testing.T) {
	t.Parallel()
	r := config.Redis{Enabled: true, Host: "", Port: 6379}
	if !errors.Is(r.Validate(), config.ErrInvalidRedisHost) {
		t.Errorf("Expected ErrInvalidRedisHost, got %v", r.Validate())
	}
}

func TestRedisValidateInvalidPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too high", 70000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := config.Redis{Enabled: true, Host: "localhost", Port: tt.port}
			if !errors.Is(r.Validate(), config.ErrInvalidRedisPort) {
				t.Errorf("Expected ErrInvalidRedisPort for port %d, got %v", tt.port, r.Validate())
			}
		})
	}
}

func TestRedisValidateValid(t *testing.T) {
	t.Parallel()
	r := config.Redis{Enabled: true, Host: "localhost", Port: 6379}
	if err := r.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

func TestRedisValidateWithFieldsMultipleErrors(t *testing.T) {
	t.Parallel()
	r := config.Redis{Enabled: true, Host: "", Port: 0}
	errs := r.ValidateWithFields()
	if len(errs) != 2 {
		t.Fatalf("Expected 2 errors, got %d", len(errs))
	}
}

// --- Database Validation ---

func TestDatabaseValidateInvalidDriver(t *testing.T) {
	t.Parallel()
	d := config.Database{Driver: "invalid", Database: "test"}
	if !errors.Is(d.Validate(), config.ErrInvalidDatabaseDriver) {
		t.Errorf("Expected ErrInvalidDatabaseDriver, got %v", d.Validate())
	}
}

func TestDatabaseValidateSQLiteNoHost(t *testing.T) {
	t.Parallel()
	d := config.Database{Driver: config.DatabaseDriverSQLite, Database: "test.db"}
	if err := d.Validate(); err != nil {
		t.Errorf("Expected nil error for SQLite without host, got %v", err)
	}
}

func TestDatabaseValidatePostgresEmptyHost(t *testing.T) {
	t.Parallel()
	d := config.Database{Driver: config.DatabaseDriverPostgres, Host: "", Port: 5432, Database: "test"}
	if !errors.Is(d.Validate(), config.ErrInvalidDatabaseHost) {
		t.Errorf("Expected ErrInvalidDatabaseHost, got %v", d.Validate())
	}
}

func TestDatabaseValidatePostgresInvalidPort(t *testing.T) {
	t.Parallel()
	d := config.Database{Driver: config.DatabaseDriverPostgres, Host: "localhost", Port: 0, Database: "test"}
	if !errors.Is(d.Validate(), config.ErrInvalidDatabasePort) {
		t.Errorf("Expected ErrInvalidDatabasePort, got %v", d.Validate())
	}
}

func TestDatabaseValidateEmptyName(t *testing.T) {
	t.Parallel()
	d := config.Database{Driver: config.DatabaseDriverSQLite, Database: ""}
	if !errors.Is(d.Validate(), config.ErrInvalidDatabaseName) {
		t.Errorf("Expected ErrInvalidDatabaseName, got %v", d.Validate())
	}
}

func TestDatabaseValidatePostgresValid(t *testing.T) {
	t.Parallel()
	d := config.Database{Driver: config.DatabaseDriverPostgres, Host: "localhost", Port: 5432, Database: "test"}
	if err := d.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

func TestDatabaseValidateMySQLValid(t *testing.T) {
	t.Parallel()
	d := config.Database{Driver: config.DatabaseDriverMySQL, Host: "localhost", Port: 3306, Database: "test"}
	if err := d.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

// --- RobotsTXT Validation ---

func TestRobotsTXTValidateInvalidMode(t *testing.T) {
	t.Parallel()
	r := config.RobotsTXT{Mode: "bogus"}
	if !errors.Is(r.Validate(), config.ErrHTTPRobotsTXTModeInvalid) {
		t.Errorf("Expected ErrHTTPRobotsTXTModeInvalid, got %v", r.Validate())
	}
}

func TestRobotsTXTValidateCustomModeEmptyContent(t *testing.T) {
	t.Parallel()
	r := config.RobotsTXT{Mode: config.RobotsTXTModeCustom, Content: ""}
	if !errors.Is(r.Validate(), config.ErrInvalidHTTPRobotsTXTContent) {
		t.Errorf("Expected ErrInvalidHTTPRobotsTXTContent, got %v", r.Validate())
	}
}

func TestRobotsTXTValidateCustomModeWithContent(t *testing.T) {
	t.Parallel()
	r := config.RobotsTXT{Mode: config.RobotsTXTModeCustom, Content: "User-agent: *\nAllow: /"}
	if err := r.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

// --- HTTP Validation ---

func TestHTTPValidateEmptyBind(t *testing.T) {
	t.Parallel()
	h := config.HTTP{Bind: "", Port: 3005, CanonicalHost: "http://localhost", RobotsTXT: config.RobotsTXT{Mode: config.RobotsTXTModeDisabled}}
	if !errors.Is(h.Validate(), config.ErrInvalidHTTPHost) {
		t.Errorf("Expected ErrInvalidHTTPHost, got %v", h.Validate())
	}
}

func TestHTTPValidateInvalidPort(t *testing.T) {
	t.Parallel()
	h := config.HTTP{Bind: "[::]", Port: -1, CanonicalHost: "http://localhost", RobotsTXT: config.RobotsTXT{Mode: config.RobotsTXTModeDisabled}}
	if !errors.Is(h.Validate(), config.ErrInvalidHTTPPort) {
		t.Errorf("Expected ErrInvalidHTTPPort, got %v", h.Validate())
	}
}

func TestHTTPValidateEmptyCanonicalHost(t *testing.T) {
	t.Parallel()
	h := config.HTTP{Bind: "[::]", Port: 3005, CanonicalHost: "", RobotsTXT: config.RobotsTXT{Mode: config.RobotsTXTModeDisabled}}
	if !errors.Is(h.Validate(), config.ErrHTTPCanonicalHostRequired) {
		t.Errorf("Expected ErrHTTPCanonicalHostRequired, got %v", h.Validate())
	}
}

func TestHTTPValidateValid(t *testing.T) {
	t.Parallel()
	h := config.HTTP{Bind: "[::]", Port: 3005, CanonicalHost: "http://localhost:3005", RobotsTXT: config.RobotsTXT{Mode: config.RobotsTXTModeDisabled}}
	if err := h.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

// --- MMDVM Validation ---

func TestMMDVMValidateEmptyBind(t *testing.T) {
	t.Parallel()
	m := config.MMDVM{Bind: "", Port: 62031}
	if !errors.Is(m.Validate(), config.ErrInvalidDMRMMDVMHost) {
		t.Errorf("Expected ErrInvalidDMRMMDVMHost, got %v", m.Validate())
	}
}

func TestMMDVMValidateInvalidPort(t *testing.T) {
	t.Parallel()
	m := config.MMDVM{Bind: "[::]", Port: 0}
	if !errors.Is(m.Validate(), config.ErrInvalidDMRMMDVMPort) {
		t.Errorf("Expected ErrInvalidDMRMMDVMPort, got %v", m.Validate())
	}
}

func TestMMDVMValidateValid(t *testing.T) {
	t.Parallel()
	m := config.MMDVM{Bind: "[::]", Port: 62031}
	if err := m.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

// --- OpenBridge Validation ---

func TestOpenBridgeValidateDisabled(t *testing.T) {
	t.Parallel()
	o := config.OpenBridge{Enabled: false}
	if err := o.Validate(); err != nil {
		t.Errorf("Expected nil error for disabled OpenBridge, got %v", err)
	}
}

func TestOpenBridgeValidateEmptyBind(t *testing.T) {
	t.Parallel()
	o := config.OpenBridge{Enabled: true, Bind: "", Port: 62035}
	if !errors.Is(o.Validate(), config.ErrInvalidDMROpenBridgeHost) {
		t.Errorf("Expected ErrInvalidDMROpenBridgeHost, got %v", o.Validate())
	}
}

func TestOpenBridgeValidateValid(t *testing.T) {
	t.Parallel()
	o := config.OpenBridge{Enabled: true, Bind: "[::]", Port: 62035}
	if err := o.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

// --- SMTP Validation ---

func TestSMTPValidateDisabled(t *testing.T) {
	t.Parallel()
	s := config.SMTP{Enabled: false}
	if err := s.Validate(); err != nil {
		t.Errorf("Expected nil error for disabled SMTP, got %v", err)
	}
}

func TestSMTPValidateEmptyHost(t *testing.T) {
	t.Parallel()
	s := config.SMTP{Enabled: true, Host: "", Port: 25, AuthMethod: config.SMTPAuthMethodNone, TLS: config.SMTPTLSNone, From: "test@test.com"}
	if !errors.Is(s.Validate(), config.ErrInvalidSMTPHost) {
		t.Errorf("Expected ErrInvalidSMTPHost, got %v", s.Validate())
	}
}

func TestSMTPValidateInvalidAuthMethod(t *testing.T) {
	t.Parallel()
	s := config.SMTP{Enabled: true, Host: "smtp.example.com", Port: 25, AuthMethod: "invalid", TLS: config.SMTPTLSNone, From: "test@test.com"}
	if !errors.Is(s.Validate(), config.ErrInvalidSMTPAuthMethod) {
		t.Errorf("Expected ErrInvalidSMTPAuthMethod, got %v", s.Validate())
	}
}

func TestSMTPValidatePlainAuthNoUsername(t *testing.T) {
	t.Parallel()
	s := config.SMTP{Enabled: true, Host: "smtp.example.com", Port: 25, AuthMethod: config.SMTPAuthMethodPlain, TLS: config.SMTPTLSNone, From: "test@test.com", Username: "", Password: "pass"}
	if !errors.Is(s.Validate(), config.ErrInvalidSMTPUsername) {
		t.Errorf("Expected ErrInvalidSMTPUsername, got %v", s.Validate())
	}
}

func TestSMTPValidateValid(t *testing.T) {
	t.Parallel()
	s := config.SMTP{Enabled: true, Host: "smtp.example.com", Port: 587, AuthMethod: config.SMTPAuthMethodPlain, TLS: config.SMTPTLSStartTLS, From: "test@test.com", Username: "user", Password: "pass"}
	if err := s.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

// --- Metrics Validation ---

func TestMetricsValidateDisabled(t *testing.T) {
	t.Parallel()
	m := config.Metrics{Enabled: false}
	if err := m.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

func TestMetricsValidateValid(t *testing.T) {
	t.Parallel()
	m := config.Metrics{Enabled: true, Bind: "[::]", Port: 9000}
	if err := m.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

// --- PProf Validation ---

func TestPProfValidateDisabled(t *testing.T) {
	t.Parallel()
	p := config.PProf{Enabled: false}
	if err := p.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

func TestPProfValidateValid(t *testing.T) {
	t.Parallel()
	p := config.PProf{Enabled: true, Bind: "[::]", Port: 6060}
	if err := p.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

// --- Full Config Validation ---

func TestConfigValidateInvalidLogLevel(t *testing.T) {
	t.Parallel()
	c := makeValidConfig()
	c.LogLevel = "invalid"
	if !errors.Is(c.Validate(), config.ErrInvalidLogLevel) {
		t.Errorf("Expected ErrInvalidLogLevel, got %v", c.Validate())
	}
}

func TestConfigValidateEmptySecret(t *testing.T) {
	t.Parallel()
	c := makeValidConfig()
	c.Secret = ""
	if !errors.Is(c.Validate(), config.ErrSecretRequired) {
		t.Errorf("Expected ErrSecretRequired, got %v", c.Validate())
	}
}

func TestConfigValidateEmptyPasswordSalt(t *testing.T) {
	t.Parallel()
	c := makeValidConfig()
	c.PasswordSalt = ""
	if !errors.Is(c.Validate(), config.ErrPasswordSaltRequired) {
		t.Errorf("Expected ErrPasswordSaltRequired, got %v", c.Validate())
	}
}

func TestConfigValidateValid(t *testing.T) {
	t.Parallel()
	c := makeValidConfig()
	if err := c.Validate(); err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

func TestConfigValidateAllLogLevels(t *testing.T) {
	t.Parallel()
	levels := []config.LogLevel{config.LogLevelDebug, config.LogLevelInfo, config.LogLevelWarn, config.LogLevelError}
	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			t.Parallel()
			c := makeValidConfig()
			c.LogLevel = level
			if err := c.Validate(); err != nil {
				t.Errorf("Expected nil error for log level %s, got %v", level, err)
			}
		})
	}
}

func TestConfigValidateWithFieldsReturnsMultipleErrors(t *testing.T) {
	t.Parallel()
	c := config.Config{
		LogLevel:     "invalid",
		Secret:       "",
		PasswordSalt: "",
		HTTP: config.HTTP{
			Bind: "",
			Port: 0,
		},
		DMR: config.DMR{
			MMDVM: config.MMDVM{
				Bind: "",
				Port: 0,
			},
		},
		Database: config.Database{
			Driver:   "invalid",
			Database: "",
		},
	}
	errs := c.ValidateWithFields()
	if len(errs) < 5 {
		t.Errorf("Expected at least 5 validation errors, got %d", len(errs))
	}
}

// --- GetDerivedSecret ---

func TestGetDerivedSecret(t *testing.T) {
	t.Parallel()
	c := config.Config{
		Secret:       "mysecret",
		PasswordSalt: "mysalt",
	}
	key := c.GetDerivedSecret()
	if len(key) != 32 {
		t.Errorf("Expected key length 32, got %d", len(key))
	}
}

func TestGetDerivedSecretDeterministic(t *testing.T) {
	t.Parallel()
	c := config.Config{
		Secret:       "mysecret",
		PasswordSalt: "mysalt",
	}
	key1 := c.GetDerivedSecret()
	key2 := c.GetDerivedSecret()
	for i := range key1 {
		if key1[i] != key2[i] {
			t.Errorf("Expected identical keys, got different at index %d", i)
			break
		}
	}
}

func TestGetDerivedSecretDifferentInputs(t *testing.T) {
	t.Parallel()
	c1 := config.Config{Secret: "secret1", PasswordSalt: "salt"}
	c2 := config.Config{Secret: "secret2", PasswordSalt: "salt"}
	key1 := c1.GetDerivedSecret()
	key2 := c2.GetDerivedSecret()
	same := true
	for i := range key1 {
		if key1[i] != key2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("Expected different keys for different secrets")
	}
}
