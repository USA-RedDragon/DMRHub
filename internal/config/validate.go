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
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
)

const (
	fieldRedisPort         = "redis.port"
	fieldDatabasePort      = "database.port"
	fieldHTTPPort          = "http.port"
	fieldDMRMMDVMPort      = "dmr.mmdvm.port"
	fieldDMRMMDVMWTPort    = "dmr.mmdvm.webtransport.port"
	fieldDMRMMDVMWTKey     = "dmr.mmdvm.webtransport.key-file"
	fieldDMROpenBridgePort = "dmr.openbridge.port"
	fieldDMROpenBridgeSrv  = "dmr.openbridge.server-id"
	fieldDMRIPSCPort       = "dmr.ipsc.port"
	fieldDMRIPSCNetworkID  = "dmr.ipsc.network-id"
	fieldSMTPPort          = "smtp.port"
	fieldMetricsPort       = "metrics.port"
	fieldPProfPort         = "pprof.port"
	fieldLogLevel          = "log-level"
	fieldSecret            = "secret"
	fieldPasswordSalt      = "password-salt"
	fieldRetentionMessages = "dmr.retention.messages"
	fieldRetentionTransm   = "dmr.retention.transmissions"
	fieldRetentionBursts   = "dmr.retention.bursts"
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
	// ErrInvalidDMRMMDVMWebTransportPort indicates that the WebTransport port is not a port, or is the MMDVM port.
	ErrInvalidDMRMMDVMWebTransportPort = errors.New("the WebTransport port must be a port other than the MMDVM port")
	// ErrInvalidPluginsPort indicates that the plugin port is not a port, or is the HTTP port.
	ErrInvalidPluginsPort = errors.New("the plugin port must be a port other than the HTTP port")
	// ErrInvalidPluginsBind indicates that the plugin listener has no address.
	ErrInvalidPluginsBind = errors.New("the plugin listener needs an address to bind to")
	// ErrInvalidAMBEServer indicates that an AMBE server is not a host and port.
	ErrInvalidAMBEServer = errors.New("enter a host and port for each server, such as ambeserver:2460")
	// ErrInvalidAMBECalls indicates that the calls at once of an AMBE server are negative.
	ErrInvalidAMBECalls = errors.New("calls at once is a whole number of 1 or more, or empty for no limit")
	// ErrPluginsKeyPair indicates that only one of the plugin certificate and key files is set.
	ErrPluginsKeyPair = errors.New("set both the plugin certificate and key files, or neither")
	// ErrDMRMMDVMWebTransportKeyPair indicates that only one of the WebTransport certificate and key files is set.
	ErrDMRMMDVMWebTransportKeyPair = errors.New("set both the WebTransport certificate and key files, or neither")
	// ErrInvalidDMROpenBridgeHost indicates that the provided DMR OpenBridge host is not valid.
	ErrInvalidDMROpenBridgeHost = errors.New("invalid DMR OpenBridge host provided")
	// ErrInvalidDMROpenBridgePort indicates that the provided DMR OpenBridge port is not valid.
	ErrInvalidDMROpenBridgePort = errors.New("invalid DMR OpenBridge port provided")
	// ErrInvalidDMROpenBridgeServerID indicates that the OpenBridge server ID is not 0 or 4 to 7 digits.
	ErrInvalidDMROpenBridgeServerID = errors.New("the OpenBridge server ID must be 0 or 4 to 7 digits")
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
	// ErrInvalidRetention indicates that a retention period is not 0 or a number of days up to MaxRetentionDays.
	ErrInvalidRetention = errors.New("retention must be 0 (keep forever) or a number of days up to 36500")
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
			Field: fieldRedisPort,
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
			Field: fieldDatabasePort,
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
			Field: fieldHTTPPort,
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
			Field: fieldDMRMMDVMPort,
			Err:   ErrInvalidDMRMMDVMPort,
		})
	}

	if wt := h.WebTransport; wt.Enabled {
		if wt.Port <= 0 || wt.Port > 65535 || wt.Port == h.Port {
			errs = append(errs, ValidationError{
				Field: fieldDMRMMDVMWTPort,
				Err:   ErrInvalidDMRMMDVMWebTransportPort,
			})
		}
		if (wt.CertFile == "") != (wt.KeyFile == "") {
			errs = append(errs, ValidationError{
				Field: fieldDMRMMDVMWTKey,
				Err:   ErrDMRMMDVMWebTransportKeyPair,
			})
		}
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
			Field: fieldDMROpenBridgePort,
			Err:   ErrInvalidDMROpenBridgePort,
		})
	}
	const minServerID, maxServerID = 1000, 9999999
	if o.ServerID != 0 && (o.ServerID < minServerID || o.ServerID > maxServerID) {
		errs = append(errs, ValidationError{
			Field: fieldDMROpenBridgeSrv,
			Err:   ErrInvalidDMROpenBridgeServerID,
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
			Field: fieldDMRIPSCPort,
			Err:   ErrInvalidIPSCPort,
		})
	}
	if o.NetworkID == 0 {
		errs = append(errs, ValidationError{
			Field: fieldDMRIPSCNetworkID,
			Err:   ErrInvalidIPSCNetworkID,
		})
	}

	return
}

// MaxPositionRetentionDays is the longest time that positions are kept.
const MaxPositionRetentionDays = 365

var (
	// ErrInvalidPositionInterval indicates a shortest report interval that
	// owners cannot pick.
	ErrInvalidPositionInterval = errors.New("the shortest report interval must be 5, 10, 15, 30 or 60 minutes")
	// ErrInvalidPositionRetention indicates a longest position retention
	// out of range.
	ErrInvalidPositionRetention = errors.New("the longest time to keep positions must be 1 to 365 days")
	// ErrInvalidAPRSCallsign indicates an APRS-IS callsign that APRS-IS
	// does not accept.
	ErrInvalidAPRSCallsign = errors.New("the APRS-IS callsign must be a callsign with an optional SSID, such as N0CALL-10")
	// ErrInvalidAPRSServer indicates an APRS-IS server that is not
	// host:port.
	ErrInvalidAPRSServer = errors.New("the APRS-IS server must be host:port")
)

// PositionIntervals returns the report intervals, in minutes, that an
// owner can pick.
func PositionIntervals() []int {
	return []int{5, 10, 15, 30, 60}
}

const (
	fieldPositionsMinInterval  = "dmr.positions.min-interval"
	fieldPositionsMaxRetention = "dmr.positions.max-retention"
)

// Validate validates the position tracking configuration.
func (p Positions) Validate() error {
	return firstValidationError(p.ValidateWithFields())
}

func (p Positions) ValidateWithFields() (errs []ValidationError) {
	if p.MinInterval != 0 && !slices.Contains(PositionIntervals(), p.MinInterval) {
		errs = append(errs, ValidationError{Field: fieldPositionsMinInterval, Err: ErrInvalidPositionInterval})
	}
	if p.MaxRetention < 0 || p.MaxRetention > MaxPositionRetentionDays {
		errs = append(errs, ValidationError{Field: fieldPositionsMaxRetention, Err: ErrInvalidPositionRetention})
	}
	a := p.APRSIS
	if a.Callsign != "" && !aprsCallsignRegex.MatchString(a.Callsign) {
		errs = append(errs, ValidationError{Field: "dmr.positions.aprs-is.callsign", Err: ErrInvalidAPRSCallsign})
	}
	if host, port, err := net.SplitHostPort(a.Server); a.Server != "" && (err != nil || host == "" || port == "") {
		errs = append(errs, ValidationError{Field: "dmr.positions.aprs-is.server", Err: ErrInvalidAPRSServer})
	}
	return
}

var aprsCallsignRegex = regexp.MustCompile(`^[A-Z0-9]{1,6}(-[0-9]{1,2})?$`)

// MaxRetentionDays is the longest retention period, in days.
const MaxRetentionDays = 36500

// Validate validates the Retention configuration.
func (r Retention) Validate() error {
	return firstValidationError(r.ValidateWithFields())
}

func (r Retention) ValidateWithFields() (errs []ValidationError) {
	for _, f := range []struct {
		field string
		days  int
	}{
		{fieldRetentionMessages, r.Messages},
		{fieldRetentionTransm, r.Transmissions},
		{fieldRetentionBursts, r.Bursts},
	} {
		if f.days < 0 || f.days > MaxRetentionDays {
			errs = append(errs, ValidationError{Field: f.field, Err: ErrInvalidRetention})
		}
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

	if retentionErrs := d.Retention.ValidateWithFields(); len(retentionErrs) > 0 {
		errs = append(errs, retentionErrs...)
	}

	errs = append(errs, d.Positions.ValidateWithFields()...)
	errs = append(errs, d.Plugins.ValidateWithFields()...)
	errs = append(errs, d.Transcoding.ValidateWithFields()...)

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
			Field: fieldSMTPPort,
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
			Field: fieldMetricsPort,
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
			Field: fieldPProfPort,
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

// Validate validates the plugin listener configuration.
func (p Plugins) Validate() error {
	return firstValidationError(p.ValidateWithFields())
}

// ValidateWithFields validates the plugin listener configuration when it is
// on.
func (p Plugins) ValidateWithFields() (errs []ValidationError) {
	if !p.Enabled {
		return nil
	}
	if p.Port <= 0 || p.Port > 65535 {
		errs = append(errs, ValidationError{Field: fieldPluginsPort, Err: ErrInvalidPluginsPort})
	}
	if p.Bind == "" {
		errs = append(errs, ValidationError{Field: "dmr.plugins.bind", Err: ErrInvalidPluginsBind})
	}
	if (p.CertFile == "") != (p.KeyFile == "") {
		errs = append(errs, ValidationError{Field: "dmr.plugins.key-file", Err: ErrPluginsKeyPair})
	}
	return errs
}

const (
	fieldPluginsPort = "dmr.plugins.port"
	fieldAMBEServers = "dmr.transcoding.ambe-servers"
)

// ValidateWithFields validates the address and calls at once of each AMBE
// server.
func (t Transcoding) ValidateWithFields() []ValidationError {
	for _, s := range t.AMBEServers {
		host, port, err := net.SplitHostPort(s.Address)
		if err != nil || host == "" {
			return []ValidationError{{Field: fieldAMBEServers, Err: ErrInvalidAMBEServer}}
		}
		if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
			return []ValidationError{{Field: fieldAMBEServers, Err: ErrInvalidAMBEServer}}
		}
		if s.Calls < 0 {
			return []ValidationError{{Field: fieldAMBEServers, Err: ErrInvalidAMBECalls}}
		}
	}
	return nil
}

func (c Config) ValidateWithFields() (errs []ValidationError) {
	if c.DMR.Plugins.Enabled && c.DMR.Plugins.Port == c.HTTP.Port {
		errs = append(errs, ValidationError{Field: fieldPluginsPort, Err: ErrInvalidPluginsPort})
	}

	if c.LogLevel != LogLevelDebug &&
		c.LogLevel != LogLevelInfo &&
		c.LogLevel != LogLevelWarn &&
		c.LogLevel != LogLevelError {
		errs = append(errs, ValidationError{
			Field: fieldLogLevel,
			Err:   ErrInvalidLogLevel,
		})
	}

	if c.Secret == "" {
		errs = append(errs, ValidationError{
			Field: fieldSecret,
			Err:   ErrSecretRequired,
		})
	}

	if c.PasswordSalt == "" {
		errs = append(errs, ValidationError{
			Field: fieldPasswordSalt,
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
