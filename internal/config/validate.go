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

func (c Config) Validate() error {
	if c.LogLevel != LogLevelDebug &&
		c.LogLevel != LogLevelInfo &&
		c.LogLevel != LogLevelWarn &&
		c.LogLevel != LogLevelError {
		return ErrInvalidLogLevel
	}

	if c.Redis.Enabled {
		if c.Redis.Host == "" {
			return ErrInvalidRedisHost
		}
		if c.Redis.Port <= 0 || c.Redis.Port > 65535 {
			return ErrInvalidRedisPort
		}
	}

	if c.Database.Driver != DatabaseDriverSQLite &&
		c.Database.Driver != DatabaseDriverPostgres &&
		c.Database.Driver != DatabaseDriverMySQL {
		return ErrInvalidDatabaseDriver
	}

	if c.Database.Driver != DatabaseDriverSQLite && c.Database.Host == "" {
		return ErrInvalidDatabaseHost
	}

	if c.Database.Driver != DatabaseDriverSQLite && (c.Database.Port <= 0 || c.Database.Port > 65535) {
		return ErrInvalidDatabasePort
	}

	if c.Database.Database == "" {
		return ErrInvalidDatabaseName
	}

	if c.Secret == "" {
		return ErrSecretRequired
	}

	if c.PasswordSalt == "" {
		return ErrPasswordSaltRequired
	}

	if c.HTTP.Bind == "" {
		return ErrInvalidHTTPHost
	}

	if c.HTTP.Port <= 0 || c.HTTP.Port > 65535 {
		return ErrInvalidHTTPPort
	}

	if c.DMR.HBRP.Bind == "" {
		return ErrInvalidDMRHBRPHost
	}

	if c.DMR.HBRP.Port <= 0 || c.DMR.HBRP.Port > 65535 {
		return ErrInvalidDMRHBRPPort
	}

	if c.DMR.OpenBridge.Enabled {
		if c.DMR.OpenBridge.Bind == "" {
			return ErrInvalidDMROpenBridgeHost
		}
		if c.DMR.OpenBridge.Port <= 0 || c.DMR.OpenBridge.Port > 65535 {
			return ErrInvalidDMROpenBridgePort
		}
	}

	if c.SMTP.Enabled {
		if c.SMTP.Host == "" {
			return ErrInvalidSMTPHost
		}
		if c.SMTP.Port <= 0 || c.SMTP.Port > 65535 {
			return ErrInvalidSMTPPort
		}
		if c.SMTP.AuthMethod != SMTPAuthMethodPlain &&
			c.SMTP.AuthMethod != SMTPAuthMethodLogin &&
			c.SMTP.AuthMethod != SMTPAuthMethodNone {
			return ErrInvalidSMTPAuthMethod
		}
		if c.SMTP.TLS != SMTPTLSNone &&
			c.SMTP.TLS != SMTPTLSStartTLS &&
			c.SMTP.TLS != SMTPTLSImplicit {
			return ErrInvalidSMTPTLS
		}
		if c.SMTP.From == "" {
			return ErrSMTPFromRequired
		}
		if c.SMTP.Username == "" && c.SMTP.AuthMethod != SMTPAuthMethodNone {
			return ErrInvalidSMTPUsername
		}
		if c.SMTP.Password == "" && c.SMTP.AuthMethod != SMTPAuthMethodNone {
			return ErrInvalidSMTPPassword
		}
	}

	if c.HTTP.RobotsTXT.Mode != RobotsTXTModeAllow &&
		c.HTTP.RobotsTXT.Mode != RobotsTXTModeDisabled &&
		c.HTTP.RobotsTXT.Mode != RobotsTXTModeCustom {
		return ErrHTTPRobotsTXTModeInvalid
	}

	if c.HTTP.RobotsTXT.Mode == RobotsTXTModeCustom && c.HTTP.RobotsTXT.Content == "" {
		return ErrInvalidHTTPRobotsTXTContent
	}

	if c.HTTP.CanonicalHost == "" {
		return ErrHTTPCanonicalHostRequired
	}

	if c.Metrics.Enabled {
		if c.Metrics.Bind == "" {
			return ErrInvalidMetricsBindAddress
		}
		if c.Metrics.Port <= 0 || c.Metrics.Port > 65535 {
			return ErrInvalidMetricsPort
		}
	}

	if c.PProf.Enabled {
		if c.PProf.Bind == "" {
			return ErrInvalidPProfBindAddress
		}
		if c.PProf.Port <= 0 || c.PProf.Port > 65535 {
			return ErrInvalidPProfPort
		}
	}

	return nil
}
