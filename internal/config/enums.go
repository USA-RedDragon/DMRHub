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

// LogLevel represents the logging level for the application.
type LogLevel string

const (
	// LogLevelDebug is the debug logging level, providing detailed information.
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo is the informational logging level, providing general information.
	LogLevelInfo LogLevel = "info"
	// LogLevelWarn is the warning logging level, indicating potential issues.
	LogLevelWarn LogLevel = "warn"
	// LogLevelError is the error logging level, indicating serious issues.
	LogLevelError LogLevel = "error"
)

// DatabaseDriver represents the type of database driver used in the application.
type DatabaseDriver string

const (
	// DatabaseDriverSQLite is the SQLite database driver.
	DatabaseDriverSQLite DatabaseDriver = "sqlite"
	// DatabaseDriverPostgres is the PostgreSQL database driver.
	DatabaseDriverPostgres DatabaseDriver = "postgres"
	// DatabaseDriverMySQL is the MySQL database driver.
	DatabaseDriverMySQL DatabaseDriver = "mysql"
)

// SMTPAuthMethod represents the authentication method used for SMTP.
type SMTPAuthMethod string

const (
	// SMTPAuthMethodPlain uses plain text authentication.
	SMTPAuthMethodPlain SMTPAuthMethod = "plain"
	// SMTPAuthMethodLogin uses login authentication.
	SMTPAuthMethodLogin SMTPAuthMethod = "login"
	// SMTPAuthMethodNone does not use authentication.
	SMTPAuthMethodNone SMTPAuthMethod = "none"
)

// SMTPTLS represents the TLS configuration for SMTP connections.
type SMTPTLS string

const (
	// SMTPTLSNone indicates no TLS is used.
	SMTPTLSNone SMTPTLS = "none"
	// SMTPTLSStartTLS indicates that STARTTLS is used for secure connections.
	SMTPTLSStartTLS SMTPTLS = "starttls"
	// SMTPTLSImplicit indicates that implicit TLS is used for secure connections.
	SMTPTLSImplicit SMTPTLS = "implicit"
)

// RobotsTXTMode represents the mode for handling robots.txt in the HTTP server.
type RobotsTXTMode string

const (
	// RobotsTXTModeAllow allows all robots to access the site.
	RobotsTXTModeAllow RobotsTXTMode = "allow"
	// RobotsTXTModeDisabled sends a robots.txt file that disallows all robots.
	RobotsTXTModeDisabled RobotsTXTMode = "disabled"
	// RobotsTXTModeCustom allows a custom robots.txt file to be served.
	RobotsTXTModeCustom RobotsTXTMode = "custom"
)
