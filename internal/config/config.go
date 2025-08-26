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

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
	"gopkg.in/yaml.v3"
)

// Config stores the application configuration.
type Config struct {
	LogLevel     LogLevel `json:"log-level,omitempty" yaml:"log-level,omitempty" name:"log-level" description:"Logging level for the application. One of debug, info, warn, or error" default:"info"`
	Redis        Redis    `json:"redis,omitempty" name:"redis" description:"Redis configuration for the application"`
	Database     Database `json:"database,omitempty" name:"database" description:"Database configuration for the application"`
	Secret       string   `json:"-" yaml:"secret" name:"secret" description:"Secret key for the application, used for signing and encryption of the user session"`
	PasswordSalt string   `json:"-" yaml:"password-salt" name:"password-salt" description:"Salt used for hashing user passwords, should be a random string of sufficient length"`
	HTTP         HTTP     `json:"http,omitempty" name:"http" description:"HTTP server configuration for the application"`
	DMR          DMR      `json:"dmr,omitempty" name:"dmr" description:"DMR server configuration for the application"`
	SMTP         SMTP     `json:"smtp,omitempty" name:"smtp" description:"SMTP configuration for sending emails"`
	NetworkName  string   `json:"network-name,omitempty" yaml:"network-name" name:"network-name" description:"Name of the DMR network, used in various places like the network status page" default:"DMRHub"`
	Metrics      Metrics  `json:"metrics,omitempty" name:"metrics" description:"Metrics configuration for the application"`
	PProf        PProf    `json:"pprof,omitempty" name:"pprof" description:"PProf configuration for the application, used for profiling and debugging purposes"`
	HIBPAPIKey   string   `json:"-" yaml:"hibp-api-key" name:"hibp-api-key" description:"API key for the Have I Been Pwned service, used for checking if passwords have been compromised"`
}

// GetDerivedSecret generates a derived secret key using PBKDF2 with the configured secret and password salt.
func (c Config) GetDerivedSecret() []byte {
	const iterations = 4096
	const keyLen = 32
	return pbkdf2.Key([]byte(c.Secret), []byte(c.PasswordSalt), iterations, keyLen, sha256.New)
}

// Metrics holds the metrics configuration.
type Metrics struct {
	Enabled        bool     `json:"enabled,omitempty" name:"enabled" description:"Enable metrics collection and export" default:"false"`
	Bind           string   `json:"bind,omitempty" name:"bind" description:"Metrics server listen address" default:"[::]"`
	Port           int      `json:"port,omitempty" name:"port" description:"Metrics server port" default:"9000"`
	TrustedProxies []string `json:"trusted-proxies,omitempty" yaml:"trusted-proxies" name:"trusted-proxies" description:"List of trusted proxy IPs for the metrics server"`
	OTLPEndpoint   string   `json:"otlp-endpoint,omitempty" yaml:"otlp-endpoint" name:"otlp-endpoint" description:"OTLP endpoint for exporting telemetry data"`
}

// PProf holds the PProf configuration.
type PProf struct {
	Enabled        bool     `json:"enabled,omitempty" name:"enabled" description:"Enable PProf profiling and debugging support" default:"false"`
	Bind           string   `json:"bind,omitempty" name:"bind" description:"PProf server listen address" default:"[::]"`
	TrustedProxies []string `json:"trusted-proxies,omitempty" yaml:"trusted-proxies" name:"trusted-proxies" description:"List of trusted proxy IPs for the PProf server"`
	Port           int      `json:"port,omitempty" name:"port" description:"PProf server port" default:"6060"`
}

// Redis holds the Redis configuration.
type Redis struct {
	Enabled  bool   `json:"enabled,omitempty" name:"enabled" description:"Enable Redis support" default:"false"`
	Host     string `json:"host,omitempty" name:"host" description:"Redis host address"`
	Port     int    `json:"port,omitempty" name:"port" description:"Redis port" default:"6379"`
	Password string `json:"-" yaml:"password" name:"password" description:"Redis password"`
}

// Database holds the database configuration.
type Database struct {
	Driver          DatabaseDriver `json:"driver,omitempty" name:"driver" description:"Database driver to use" default:"sqlite"`
	Database        string         `json:"database,omitempty" name:"database" description:"Database name or path" default:"DMRHub.db"`
	Host            string         `json:"host,omitempty" name:"host" description:"Database host address"`
	Port            int            `json:"port,omitempty" name:"port" description:"Database port"`
	Username        string         `json:"username,omitempty" name:"username" description:"Database username"`
	Password        string         `json:"-" yaml:"password" name:"password" description:"Database password"`
	ExtraParameters []string       `json:"extra-parameters,omitempty" yaml:"extra-parameters" name:"extra-parameters" description:"Additional parameters for the database connection, e.g., sslmode=disable" default:"_pragma=foreign_keys(1),_pragma=journal_mode(WAL)"`
}

// HTTP holds the HTTP server configuration.
type HTTP struct {
	Bind           string    `json:"bind,omitempty" name:"bind" description:"HTTP server listen address" default:"[::]"`
	Port           int       `json:"port,omitempty" name:"port" description:"HTTP server port" default:"3005"`
	RobotsTXT      RobotsTXT `json:"robots-txt,omitempty" yaml:"robots-txt" name:"robots-txt" description:"Robots.txt configuration for the HTTP server"`
	CORS           CORS      `json:"cors,omitempty" name:"cors" description:"CORS configuration for the HTTP server"`
	TrustedProxies []string  `json:"trusted-proxies,omitempty" yaml:"trusted-proxies" name:"trusted-proxies" description:"List of trusted proxy IPs for the HTTP server"`
	CanonicalHost  string    `json:"canonical-host,omitempty" yaml:"canonical-host" name:"canonical-host" description:"Canonical host for the HTTP server, used for generating absolute URLs"`
}

// CORS holds the CORS configuration for the HTTP server.
type CORS struct {
	Enabled bool     `json:"enabled,omitempty" name:"enabled" description:"Enable CORS support for the HTTP server" default:"false"`
	Hosts   []string `json:"extra-hosts,omitempty" yaml:"extra-hosts" name:"extra-hosts" description:"List of allowed CORS hosts"`
}

// RobotsTXT holds the configuration for the robots.txt file served by the HTTP server.
type RobotsTXT struct {
	Mode    RobotsTXTMode `json:"mode,omitempty" name:"mode" description:"Mode for serving robots.txt. One of allow, disabled, or custom" default:"disabled"`
	Content string        `json:"content,omitempty" name:"content" description:"Content of the robots.txt file"`
}

// DMR holds the DMR server configuration.
type DMR struct {
	HBRP       HBRP       `json:"hbrp,omitempty" name:"hbrp" description:"HBRP server configuration for DMR"`
	OpenBridge OpenBridge `json:"openbridge,omitempty" name:"openbridge" description:"OpenBridge server configuration for DMR"`
}

// HBRP holds the configuration for the HBRP server.
type HBRP struct {
	Bind string `json:"bind,omitempty" name:"bind" description:"HBRP server listen address" default:"[::]"`
	Port int    `json:"port,omitempty" name:"port" description:"HBRP server port" default:"62031"`
}

// OpenBridge holds the configuration for the OpenBridge server.
type OpenBridge struct {
	Enabled bool   `json:"enabled,omitempty" name:"enabled" description:"Enable experimental and broken OpenBridge server support" default:"false"`
	Bind    string `json:"bind,omitempty" name:"bind" description:"OpenBridge server listen address" default:"[::]"`
	Port    int    `json:"port,omitempty" name:"port" description:"OpenBridge server port" default:"62035"`
}

// SMTP holds the SMTP configuration.
type SMTP struct {
	Enabled    bool           `json:"enabled,omitempty" name:"enabled" description:"Enable SMTP support for sending emails" default:"false"`
	Host       string         `json:"host,omitempty" name:"host" description:"SMTP server host address"`
	Port       int            `json:"port,omitempty" name:"port" description:"SMTP server port" default:"25"`
	TLS        SMTPTLS        `json:"tls,omitempty" name:"tls" description:"SMTP TLS mode" default:"none"`
	Username   string         `json:"username,omitempty" name:"username" description:"SMTP server username"`
	Password   string         `json:"-" yaml:"password" name:"password" description:"SMTP server password"`
	From       string         `json:"from,omitempty" name:"from" description:"Email address to use as the sender"`
	AuthMethod SMTPAuthMethod `json:"auth-method,omitempty" yaml:"auth-method" name:"auth-method" description:"SMTP authentication method. One of none, plain, or login" default:"none"`
}

func (c Config) Save() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get user config directory: %w", err)
	}
	if err := os.MkdirAll(path.Join(configDir, "DMRHub"), 0750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	f, err := os.Create(filepath.Clean(path.Join(configDir, "DMRHub", "config.yaml")))
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("failed to close config file: %v\n", err)
		}
	}()
	enc := yaml.NewEncoder(f)
	defer func() {
		if err := enc.Close(); err != nil {
			fmt.Printf("failed to close config encoder: %v\n", err)
		}
	}()
	if err := enc.Encode(c); err != nil {
		return fmt.Errorf("failed to encode config to file: %w", err)
	}
	return nil
}
