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

package config

import (
	"errors"
	"net/url"
)

// isValidURL checks that s is a parseable URL with both a scheme and host.
func isValidURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}

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
	// ErrInvalidDMRMMDVMHost indicates that the provided DMR MMDVM host is not valid.
	ErrInvalidDMRMMDVMHost = errors.New("invalid DMR MMDVM host provided")
	// ErrInvalidDMRMMDVMPort indicates that the provided DMR MMDVM port is not valid.
	ErrInvalidDMRMMDVMPort = errors.New("invalid DMR MMDVM port provided")
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
	// ErrHTTPCanonicalHostRequired indicates that the canonical host is required for the HTTP server.
	ErrHTTPCanonicalHostRequired = errors.New("canonical host is required for generating absolute URLs in the HTTP server")
	// ErrInvalidIPSCBindAddress indicates that the provided IPSC server bind address is not valid.
	ErrInvalidIPSCBindAddress = errors.New("invalid IPSC server bind address provided")
	// ErrInvalidIPSCPort indicates that the provided IPSC server port is not valid.
	ErrInvalidIPSCPort = errors.New("invalid IPSC server port provided")
	// ErrInvalidIPSCNetworkID indicates that the IPSC network ID is required when IPSC is enabled.
	ErrInvalidIPSCNetworkID = errors.New("IPSC network ID is required when IPSC is enabled")
	// ErrInvalidDMRRadioIDURL indicates that the provided DMR radio ID URL is not valid.
	ErrInvalidDMRRadioIDURL = errors.New("invalid DMR radio ID URL provided")
	// ErrInvalidDMRRepeaterIDURL indicates that the provided DMR repeater ID URL is not valid.
	ErrInvalidDMRRepeaterIDURL = errors.New("invalid DMR repeater ID URL provided")
)

// Validate validates the Redis configuration.
func (r Redis) Validate() error {
	return firstValidationError(r.ValidateWithFields())
}

func (r Redis) ValidateWithFields() (errs []ValidationError) {
	if !r.Enabled {
		return nil
	}

	if r.Host == "" {
		errs = append(errs, ValidationError{
			Field: "redis.host",
			Err:   ErrInvalidRedisHost,
		})
	}
	if r.Port <= 0 || r.Port > 65535 {
		errs = append(errs, ValidationError{
			Field: "redis.port",
			Err:   ErrInvalidRedisPort,
		})
	}

	return
}

// Validate validates the Database configuration.
func (d Database) Validate() error {
	return firstValidationError(d.ValidateWithFields())
}

func (d Database) ValidateWithFields() (errs []ValidationError) {
	if d.Driver != DatabaseDriverSQLite &&
		d.Driver != DatabaseDriverPostgres &&
		d.Driver != DatabaseDriverMySQL {
		errs = append(errs, ValidationError{
			Field: "database.driver",
			Err:   ErrInvalidDatabaseDriver,
		})
	}

	if d.Driver != DatabaseDriverSQLite && d.Host == "" {
		errs = append(errs, ValidationError{
			Field: "database.host",
			Err:   ErrInvalidDatabaseHost,
		})
	}

	if d.Driver != DatabaseDriverSQLite && (d.Port <= 0 || d.Port > 65535) {
		errs = append(errs, ValidationError{
			Field: "database.port",
			Err:   ErrInvalidDatabasePort,
		})
	}

	if d.Database == "" {
		errs = append(errs, ValidationError{
			Field: "database.name",
			Err:   ErrInvalidDatabaseName,
		})
	}

	return
}

// Validate validates the RobotsTXT configuration.
func (r RobotsTXT) Validate() error {
	return firstValidationError(r.ValidateWithFields())
}

func (r RobotsTXT) ValidateWithFields() (errs []ValidationError) {
	if r.Mode != RobotsTXTModeAllow &&
		r.Mode != RobotsTXTModeDisabled &&
		r.Mode != RobotsTXTModeCustom {
		errs = append(errs, ValidationError{
			Field: "http.robots-txt.mode",
			Err:   ErrHTTPRobotsTXTModeInvalid,
		})
	}

	if r.Mode == RobotsTXTModeCustom && r.Content == "" {
		errs = append(errs, ValidationError{
			Field: "http.robots-txt.content",
			Err:   ErrInvalidHTTPRobotsTXTContent,
		})
	}

	return
}

// Validate validates the HTTP configuration.
func (h HTTP) Validate() error {
	return firstValidationError(h.ValidateWithFields())
}

func (h HTTP) ValidateWithFields() (errs []ValidationError) {
	if h.Bind == "" {
		errs = append(errs, ValidationError{
			Field: "http.bind",
			Err:   ErrInvalidHTTPHost,
		})
	}

	if h.Port <= 0 || h.Port > 65535 {
		errs = append(errs, ValidationError{
			Field: "http.port",
			Err:   ErrInvalidHTTPPort,
		})
	}

	if h.CanonicalHost == "" {
		errs = append(errs, ValidationError{
			Field: "http.canonical-host",
			Err:   ErrHTTPCanonicalHostRequired,
		})
	}

	if robotsTXTErrs := h.RobotsTXT.ValidateWithFields(); len(robotsTXTErrs) > 0 {
		errs = append(errs, robotsTXTErrs...)
	}

	return
}

// Validate validates the MMDVM configuration.
func (h MMDVM) Validate() error {
	return firstValidationError(h.ValidateWithFields())
}

func (h MMDVM) ValidateWithFields() (errs []ValidationError) {
	if h.Bind == "" {
		errs = append(errs, ValidationError{
			Field: "dmr.mmdvm.bind",
			Err:   ErrInvalidDMRMMDVMHost,
		})
	}

	if h.Port <= 0 || h.Port > 65535 {
		errs = append(errs, ValidationError{
			Field: "dmr.mmdvm.port",
			Err:   ErrInvalidDMRMMDVMPort,
		})
	}

	return
}

// Validate validates the OpenBridge configuration.
func (o OpenBridge) Validate() error {
	return firstValidationError(o.ValidateWithFields())
}

func (o OpenBridge) ValidateWithFields() (errs []ValidationError) {
	if !o.Enabled {
		return nil
	}

	if o.Bind == "" {
		errs = append(errs, ValidationError{
			Field: "dmr.openbridge.bind",
			Err:   ErrInvalidDMROpenBridgeHost,
		})
	}
	if o.Port <= 0 || o.Port > 65535 {
		errs = append(errs, ValidationError{
			Field: "dmr.openbridge.port",
			Err:   ErrInvalidDMROpenBridgePort,
		})
	}

	return
}

func (o IPSC) Validate() error {
	return firstValidationError(o.ValidateWithFields())
}

func (o IPSC) ValidateWithFields() (errs []ValidationError) {
	if !o.Enabled {
		return nil
	}

	if o.Bind == "" {
		errs = append(errs, ValidationError{
			Field: "dmr.ipsc.bind",
			Err:   ErrInvalidIPSCBindAddress,
		})
	}
	if o.Port <= 0 || o.Port > 65535 {
		errs = append(errs, ValidationError{
			Field: "dmr.ipsc.port",
			Err:   ErrInvalidIPSCPort,
		})
	}
	if o.NetworkID == 0 {
		errs = append(errs, ValidationError{
			Field: "dmr.ipsc.network-id",
			Err:   ErrInvalidIPSCNetworkID,
		})
	}

	return
}

// Validate validates the DMR configuration.
func (d DMR) Validate() error {
	return firstValidationError(d.ValidateWithFields())
}

func (d DMR) ValidateWithFields() (errs []ValidationError) {
	if mmdvmErrs := d.MMDVM.ValidateWithFields(); len(mmdvmErrs) > 0 {
		errs = append(errs, mmdvmErrs...)
	}

	if openBridgeErrs := d.OpenBridge.ValidateWithFields(); len(openBridgeErrs) > 0 {
		errs = append(errs, openBridgeErrs...)
	}

	if ipscErrs := d.IPSC.ValidateWithFields(); len(ipscErrs) > 0 {
		errs = append(errs, ipscErrs...)
	}

	if !isValidURL(d.RadioIDURL) {
		errs = append(errs, ValidationError{
			Field: "dmr.radio-id-url",
			Err:   ErrInvalidDMRRadioIDURL,
		})
	}

	if !isValidURL(d.RepeaterIDURL) {
		errs = append(errs, ValidationError{
			Field: "dmr.repeater-id-url",
			Err:   ErrInvalidDMRRepeaterIDURL,
		})
	}

	return
}

// Validate validates the SMTP configuration.
func (s SMTP) Validate() error {
	return firstValidationError(s.ValidateWithFields())
}

func (s SMTP) ValidateWithFields() (errs []ValidationError) {
	if !s.Enabled {
		return nil
	}

	if s.Host == "" {
		errs = append(errs, ValidationError{
			Field: "smtp.host",
			Err:   ErrInvalidSMTPHost,
		})
	}
	if s.Port <= 0 || s.Port > 65535 {
		errs = append(errs, ValidationError{
			Field: "smtp.port",
			Err:   ErrInvalidSMTPPort,
		})
	}
	if s.AuthMethod != SMTPAuthMethodPlain &&
		s.AuthMethod != SMTPAuthMethodLogin &&
		s.AuthMethod != SMTPAuthMethodNone {
		errs = append(errs, ValidationError{
			Field: "smtp.auth-method",
			Err:   ErrInvalidSMTPAuthMethod,
		})
	}
	if s.TLS != SMTPTLSNone &&
		s.TLS != SMTPTLSStartTLS &&
		s.TLS != SMTPTLSImplicit {
		errs = append(errs, ValidationError{
			Field: "smtp.tls",
			Err:   ErrInvalidSMTPTLS,
		})
	}
	if s.From == "" {
		errs = append(errs, ValidationError{
			Field: "smtp.from",
			Err:   ErrSMTPFromRequired,
		})
	}
	if s.Username == "" && s.AuthMethod != SMTPAuthMethodNone {
		errs = append(errs, ValidationError{
			Field: "smtp.username",
			Err:   ErrInvalidSMTPUsername,
		})
	}
	if s.Password == "" && s.AuthMethod != SMTPAuthMethodNone {
		errs = append(errs, ValidationError{
			Field: "smtp.password",
			Err:   ErrInvalidSMTPPassword,
		})
	}

	return
}

// Validate validates the Metrics configuration.
func (m Metrics) Validate() error {
	return firstValidationError(m.ValidateWithFields())
}

func (m Metrics) ValidateWithFields() (errs []ValidationError) {
	if !m.Enabled {
		return nil
	}

	if m.Bind == "" {
		errs = append(errs, ValidationError{
			Field: "metrics.bind",
			Err:   ErrInvalidMetricsBindAddress,
		})
	}
	if m.Port <= 0 || m.Port > 65535 {
		errs = append(errs, ValidationError{
			Field: "metrics.port",
			Err:   ErrInvalidMetricsPort,
		})
	}

	return
}

// Validate validates the PProf configuration.
func (p PProf) Validate() error {
	return firstValidationError(p.ValidateWithFields())
}

func (p PProf) ValidateWithFields() (errs []ValidationError) {
	if !p.Enabled {
		return nil
	}

	if p.Bind == "" {
		errs = append(errs, ValidationError{
			Field: "pprof.bind",
			Err:   ErrInvalidPProfBindAddress,
		})
	}
	if p.Port <= 0 || p.Port > 65535 {
		errs = append(errs, ValidationError{
			Field: "pprof.port",
			Err:   ErrInvalidPProfPort,
		})
	}

	return
}

func (c Config) Validate() error {
	return firstValidationError(c.ValidateWithFields())
}

// ValidationError represents a validation error with the field path and the associated error.
type ValidationError struct {
	Field string
	Err   error
}

// firstValidationError returns the error from the first ValidationError, or nil if no errors.
func firstValidationError(errs []ValidationError) error {
	if len(errs) > 0 {
		return errs[0].Err
	}
	return nil
}

func (c Config) ValidateWithFields() (errs []ValidationError) {
	if c.LogLevel != LogLevelDebug &&
		c.LogLevel != LogLevelInfo &&
		c.LogLevel != LogLevelWarn &&
		c.LogLevel != LogLevelError {
		errs = append(errs, ValidationError{
			Field: "log-level",
			Err:   ErrInvalidLogLevel,
		})
	}

	if c.Secret == "" {
		errs = append(errs, ValidationError{
			Field: "secret",
			Err:   ErrSecretRequired,
		})
	}

	if c.PasswordSalt == "" {
		errs = append(errs, ValidationError{
			Field: "password-salt",
			Err:   ErrPasswordSaltRequired,
		})
	}

	if redisErrs := c.Redis.ValidateWithFields(); len(redisErrs) > 0 {
		errs = append(errs, redisErrs...)
	}

	if dbErrs := c.Database.ValidateWithFields(); len(dbErrs) > 0 {
		errs = append(errs, dbErrs...)
	}

	if httpErrs := c.HTTP.ValidateWithFields(); len(httpErrs) > 0 {
		errs = append(errs, httpErrs...)
	}

	if dmrErrs := c.DMR.ValidateWithFields(); len(dmrErrs) > 0 {
		errs = append(errs, dmrErrs...)
	}

	if smtpErrs := c.SMTP.ValidateWithFields(); len(smtpErrs) > 0 {
		errs = append(errs, smtpErrs...)
	}

	if metricsErrs := c.Metrics.ValidateWithFields(); len(metricsErrs) > 0 {
		errs = append(errs, metricsErrs...)
	}

	if pprofErrs := c.PProf.ValidateWithFields(); len(pprofErrs) > 0 {
		errs = append(errs, pprofErrs...)
	}

	return
}
