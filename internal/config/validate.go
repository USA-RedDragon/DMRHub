// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package config

import "errors"

var (
	// ErrInvalidLogLevel indicates that the provided log level is not valid.
	ErrInvalidLogLevel = errors.New("invalid log level provided")
	// ErrInvalidRedisHost indicates that the provided Redis host is not valid.
	ErrInvalidRedisHost = errors.New("invalid Redis host provided")
	// ErrInvalidRedisPort indicates that the provided Redis port is not valid.
	ErrInvalidRedisPort = errors.New("invalid Redis port provided")
	// ErrInvalidDatabaseDriver indicates that the provided database driver is not valid.
	ErrInvalidDatabaseDriver = errors.New("invalid database driver provided")
	// ErrInvalidDatabaseHost indicates that the provided database host is not valid.
	ErrInvalidDatabaseHost = errors.New("invalid database host provided")
	// ErrInvalidDatabasePort indicates that the provided database port is not valid.
	ErrInvalidDatabasePort = errors.New("invalid database port provided")
	// ErrInvalidDatabaseName indicates that the provided database name is not valid.
	ErrInvalidDatabaseName = errors.New("invalid database name provided")
	// ErrSecretRequired indicates that the secret key is required for the application.
	ErrSecretRequired = errors.New("secret key is required for the application")
	// ErrPasswordSaltRequired indicates that the password salt is required for hashing user passwords.
	ErrPasswordSaltRequired = errors.New("password salt is required for hashing user passwords")
	// ErrInvalidHTTPHost indicates that the provided HTTP host is not valid.
	ErrInvalidHTTPHost = errors.New("invalid HTTP host provided")
	// ErrInvalidHTTPPort indicates that the provided HTTP port is not valid.
	ErrInvalidHTTPPort = errors.New("invalid HTTP port provided")
	// ErrInvalidDMRHBRPHost indicates that the provided DMR HBRP host is not valid.
	ErrInvalidDMRHBRPHost = errors.New("invalid DMR HBRP host provided")
	// ErrInvalidDMRHBRPPort indicates that the provided DMR HBRP port is not valid.
	ErrInvalidDMRHBRPPort = errors.New("invalid DMR HBRP port provided")
	// ErrInvalidDMROpenBridgeHost indicates that the provided DMR OpenBridge host is not valid.
	ErrInvalidDMROpenBridgeHost = errors.New("invalid DMR OpenBridge host provided")
	// ErrInvalidDMROpenBridgePort indicates that the provided DMR OpenBridge port is not valid.
	ErrInvalidDMROpenBridgePort = errors.New("invalid DMR OpenBridge port provided")
	// ErrInvalidMetricsBindAddress indicates that the provided metrics server bind address is not valid.
	ErrInvalidMetricsBindAddress = errors.New("invalid metrics server bind address provided")
	// ErrInvalidMetricsPort indicates that the provided metrics server port is not valid.
	ErrInvalidMetricsPort = errors.New("invalid metrics server port provided")
	// ErrInvalidPProfBindAddress indicates that the provided PProf server bind address is not valid.
	ErrInvalidPProfBindAddress = errors.New("invalid PProf server bind address provided")
	// ErrInvalidPProfPort indicates that the provided PProf server port is not valid.
	ErrInvalidPProfPort = errors.New("invalid PProf server port provided")
	// ErrInvalidSMTPHost indicates that the provided SMTP host is not valid.
	ErrInvalidSMTPHost = errors.New("invalid SMTP host provided")
	// ErrInvalidSMTPPort indicates that the provided SMTP port is not valid.
	ErrInvalidSMTPPort = errors.New("invalid SMTP port provided")
	// ErrInvalidSMTPUsername indicates that the SMTP username is required when SMTP authentication is enabled.
	ErrInvalidSMTPUsername = errors.New("SMTP username is required when SMTP authentication is enabled")
	// ErrInvalidSMTPPassword indicates that the SMTP password is required when SMTP authentication is enabled.
	ErrInvalidSMTPPassword = errors.New("SMTP password is required when SMTP authentication is enabled")
	// ErrInvalidSMTPAuthMethod indicates that the provided SMTP authentication method is not valid.
	ErrInvalidSMTPAuthMethod = errors.New("invalid SMTP authentication method provided")
	// ErrInvalidSMTPTLS indicates that the provided SMTP TLS setting is not valid.
	ErrInvalidSMTPTLS = errors.New("invalid SMTP TLS setting provided")
	// ErrSMTPFromRequired indicates that the 'from' address is required when SMTP is enabled.
	ErrSMTPFromRequired = errors.New("SMTP 'from' address is required when SMTP is enabled")
	// ErrHTTPRobotsTXTModeInvalid indicates that the provided robots.txt mode is not valid.
	ErrHTTPRobotsTXTModeInvalid = errors.New("invalid robots.txt mode provided, must be one of allow, disabled, or custom")
	// ErrInvalidHTTPRobotsTXTContent indicates that the robots.txt content is required when the mode is custom.
	ErrInvalidHTTPRobotsTXTContent = errors.New("invalid robots.txt content provided, must be non-empty when mode is custom")
	// ErrInvalidMetricsBindAddress indicates that the provided metrics server bind address is not valid.
	ErrHTTPCanonicalHostRequired = errors.New("canonical host is required for generating absolute URLs in the HTTP server")
)

// Validate validates the Redis configuration.
func (r Redis) Validate() error {
	if !r.Enabled {
		return nil
	}

	if r.Host == "" {
		return ErrInvalidRedisHost
	}
	if r.Port <= 0 || r.Port > 65535 {
		return ErrInvalidRedisPort
	}

	return nil
}

// Validate validates the Database configuration.
func (d Database) Validate() error {
	if d.Driver != DatabaseDriverSQLite &&
		d.Driver != DatabaseDriverPostgres &&
		d.Driver != DatabaseDriverMySQL {
		return ErrInvalidDatabaseDriver
	}

	if d.Driver != DatabaseDriverSQLite && d.Host == "" {
		return ErrInvalidDatabaseHost
	}

	if d.Driver != DatabaseDriverSQLite && (d.Port <= 0 || d.Port > 65535) {
		return ErrInvalidDatabasePort
	}

	if d.Database == "" {
		return ErrInvalidDatabaseName
	}

	return nil
}

// Validate validates the RobotsTXT configuration.
func (r RobotsTXT) Validate() error {
	if r.Mode != RobotsTXTModeAllow &&
		r.Mode != RobotsTXTModeDisabled &&
		r.Mode != RobotsTXTModeCustom {
		return ErrHTTPRobotsTXTModeInvalid
	}

	if r.Mode == RobotsTXTModeCustom && r.Content == "" {
		return ErrInvalidHTTPRobotsTXTContent
	}

	return nil
}

// Validate validates the HTTP configuration.
func (h HTTP) Validate() error {
	if h.Bind == "" {
		return ErrInvalidHTTPHost
	}

	if h.Port <= 0 || h.Port > 65535 {
		return ErrInvalidHTTPPort
	}

	if h.CanonicalHost == "" {
		return ErrHTTPCanonicalHostRequired
	}

	if err := h.RobotsTXT.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate validates the HBRP configuration.
func (h HBRP) Validate() error {
	if h.Bind == "" {
		return ErrInvalidDMRHBRPHost
	}

	if h.Port <= 0 || h.Port > 65535 {
		return ErrInvalidDMRHBRPPort
	}

	return nil
}

// Validate validates the OpenBridge configuration.
func (o OpenBridge) Validate() error {
	if !o.Enabled {
		return nil
	}

	if o.Bind == "" {
		return ErrInvalidDMROpenBridgeHost
	}
	if o.Port <= 0 || o.Port > 65535 {
		return ErrInvalidDMROpenBridgePort
	}

	return nil
}

// Validate validates the DMR configuration.
func (d DMR) Validate() error {
	if err := d.HBRP.Validate(); err != nil {
		return err
	}

	if err := d.OpenBridge.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate validates the SMTP configuration.
func (s SMTP) Validate() error {
	if !s.Enabled {
		return nil
	}

	if s.Host == "" {
		return ErrInvalidSMTPHost
	}
	if s.Port <= 0 || s.Port > 65535 {
		return ErrInvalidSMTPPort
	}
	if s.AuthMethod != SMTPAuthMethodPlain &&
		s.AuthMethod != SMTPAuthMethodLogin &&
		s.AuthMethod != SMTPAuthMethodNone {
		return ErrInvalidSMTPAuthMethod
	}
	if s.TLS != SMTPTLSNone &&
		s.TLS != SMTPTLSStartTLS &&
		s.TLS != SMTPTLSImplicit {
		return ErrInvalidSMTPTLS
	}
	if s.From == "" {
		return ErrSMTPFromRequired
	}
	if s.Username == "" && s.AuthMethod != SMTPAuthMethodNone {
		return ErrInvalidSMTPUsername
	}
	if s.Password == "" && s.AuthMethod != SMTPAuthMethodNone {
		return ErrInvalidSMTPPassword
	}

	return nil
}

// Validate validates the Metrics configuration.
func (m Metrics) Validate() error {
	if !m.Enabled {
		return nil
	}

	if m.Bind == "" {
		return ErrInvalidMetricsBindAddress
	}
	if m.Port <= 0 || m.Port > 65535 {
		return ErrInvalidMetricsPort
	}

	return nil
}

// Validate validates the PProf configuration.
func (p PProf) Validate() error {
	if !p.Enabled {
		return nil
	}

	if p.Bind == "" {
		return ErrInvalidPProfBindAddress
	}
	if p.Port <= 0 || p.Port > 65535 {
		return ErrInvalidPProfPort
	}

	return nil
}

func (c Config) Validate() error {
	if c.LogLevel != LogLevelDebug &&
		c.LogLevel != LogLevelInfo &&
		c.LogLevel != LogLevelWarn &&
		c.LogLevel != LogLevelError {
		return ErrInvalidLogLevel
	}

	if c.Secret == "" {
		return ErrSecretRequired
	}

	if c.PasswordSalt == "" {
		return ErrPasswordSaltRequired
	}

	if err := c.Redis.Validate(); err != nil {
		return err
	}

	if err := c.Database.Validate(); err != nil {
		return err
	}

	if err := c.HTTP.Validate(); err != nil {
		return err
	}

	if err := c.DMR.Validate(); err != nil {
		return err
	}

	if err := c.SMTP.Validate(); err != nil {
		return err
	}

	if err := c.Metrics.Validate(); err != nil {
		return err
	}

	if err := c.PProf.Validate(); err != nil {
		return err
	}

	return nil
}
