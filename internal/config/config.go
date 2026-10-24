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
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"golang.org/x/crypto/pbkdf2"
)

// Config stores the application configuration.
//
//go:generate go tool configulator -type Config
type Config struct {
	LogLevel     LogLevel `json:"log-level,omitempty" yaml:"log-level,omitempty" name:"log-level" description:"Logging level for the application. One of debug, info, warn, or error" default:"info"`
	Redis        Redis    `json:"redis,omitempty" name:"redis" description:"Redis configuration for the application"`
	Database     Database `json:"database,omitempty" name:"database" description:"Database configuration for the application"`
	Secret       string   `json:"-" yaml:"secret" name:"secret" description:"Secret key for the application, used for signing and encryption of the user session. Required; use a random value of at least 15 characters"`
	PasswordSalt string   `json:"-" yaml:"password-salt" name:"password-salt" description:"Salt used for hashing user passwords. Required; use a random value of at least 15 characters, different from secret"`
	HTTP         HTTP     `json:"http,omitempty" name:"http" description:"HTTP server configuration for the application"`
	DMR          DMR      `json:"dmr,omitempty" name:"dmr" description:"DMR server configuration for the application"`
	SMTP         SMTP     `json:"smtp,omitempty" name:"smtp" description:"SMTP configuration for sending emails"`
	NetworkName  string   `json:"network-name,omitempty" yaml:"network-name" name:"network-name" description:"Name of the DMR network, shown in the web interface" default:"DMRHub"`
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
	OTLPEndpoint   string   `json:"otlp-endpoint,omitempty" yaml:"otlp-endpoint" name:"otlp-endpoint" description:"OTLP endpoint for exporting OpenTelemetry tracing data"`
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
	Driver          DatabaseDriver `json:"driver,omitempty" name:"driver" description:"Database driver to use. One of sqlite, postgres, or mysql" default:"sqlite"`
	Database        string         `json:"database,omitempty" name:"database" description:"Database name or path" default:"DMRHub.db"`
	Host            string         `json:"host,omitempty" name:"host" description:"Database host address (postgres and mysql only)"`
	Port            int            `json:"port,omitempty" name:"port" description:"Database port (postgres and mysql only)"`
	Username        string         `json:"username,omitempty" name:"username" description:"Database username (postgres and mysql only)"`
	Password        string         `json:"-" yaml:"password" name:"password" description:"Database password (postgres and mysql only)"`
	ExtraParameters []string       `json:"extra-parameters,omitempty" yaml:"extra-parameters" name:"extra-parameters" description:"Additional parameters for the database connection, e.g., sslmode=disable" default:"_pragma=foreign_keys(1),_pragma=journal_mode(WAL)"`
}

// HTTP holds the HTTP server configuration.
type HTTP struct {
	Bind           string    `json:"bind,omitempty" name:"bind" description:"HTTP server listen address" default:"[::]"`
	Port           int       `json:"port,omitempty" name:"port" description:"HTTP server port" default:"3005"`
	RobotsTXT      RobotsTXT `json:"robots-txt,omitempty" yaml:"robots-txt" name:"robots-txt" description:"Robots.txt configuration for the HTTP server"`
	CORS           CORS      `json:"cors,omitempty" name:"cors" description:"CORS configuration for the HTTP server"`
	TrustedProxies []string  `json:"trusted-proxies,omitempty" yaml:"trusted-proxies" name:"trusted-proxies" description:"List of trusted proxy IPs for the HTTP server"`
	CanonicalHost  string    `json:"canonical-host,omitempty" yaml:"canonical-host" name:"canonical-host" description:"URL the HTTP server is reached at, used for generating absolute URLs, e.g. https://dmrhub.example.com. Required"`
}

// CORS holds the CORS configuration for the HTTP server.
type CORS struct {
	Enabled bool     `json:"enabled,omitempty" name:"enabled" description:"Enable CORS support for the HTTP server" default:"false"`
	Hosts   []string `json:"extra-hosts,omitempty" yaml:"extra-hosts" name:"extra-hosts" description:"List of additional allowed CORS origins"`
}

// RobotsTXT holds the configuration for the robots.txt file served by the HTTP server.
type RobotsTXT struct {
	Mode    RobotsTXTMode `json:"mode,omitempty" name:"mode" description:"Mode for serving robots.txt. One of allow, disabled, or custom" default:"disabled"`
	Content string        `json:"content,omitempty" name:"content" description:"Content of the robots.txt file when mode is custom"`
}

// DMR holds the DMR server configuration.
type DMR struct {
	MMDVM                    MMDVM      `json:"mmdvm,omitempty" name:"mmdvm" description:"MMDVM server configuration for DMR"`
	OpenBridge               OpenBridge `json:"openbridge,omitempty" name:"openbridge" description:"OpenBridge server configuration for DMR"`
	IPSC                     IPSC       `json:"ipsc,omitempty" name:"ipsc" description:"IPSC server configuration for Motorola DMR repeaters"`
	DisableRadioIDValidation bool       `json:"disable-radio-id-validation,omitempty" name:"disable-radio-id-validation" description:"Disable validation of radio IDs in DMR packets, allowing any 7- to 9-digit number to be used as a radio ID" default:"false"`
	RadioIDURL               string     `json:"radio-id-url,omitempty" name:"radio-id-url" description:"URL to fetch radio ID information for validation and display purposes. Expected JSON format is the same as RadioID.net." default:"https://www.radioid.net/static/users.json"`
	RepeaterIDURL            string     `json:"repeater-id-url,omitempty" name:"repeater-id-url" description:"URL to fetch repeater information for validation and display purposes. Expected JSON format is the same as RadioID.net." default:"https://www.radioid.net/static/rptrs.json"`
	Retention                Retention  `json:"retention" name:"retention" description:"How long DMRHub keeps its records"`
	Positions                Positions  `json:"positions" name:"positions" description:"Position tracking of the radios whose owners turn it on"`
	Plugins                  Plugins    `json:"plugins" name:"plugins" description:"Listener of the plugins, which answer the calls to the IDs an admin gives them"`
}

// Plugins holds the configuration of the listener that plugins connect to.
type Plugins struct {
	Enabled  bool   `json:"enabled" name:"enabled" description:"Accept plugins" default:"false"`
	Bind     string `json:"bind" name:"bind" description:"Address the plugin listener binds to" default:"[::]"`
	Port     int    `json:"port" name:"port" description:"TCP port that plugins connect to" default:"3006"`
	CertFile string `json:"cert-file" yaml:"cert-file" name:"cert-file" description:"TLS certificate file of the plugin listener. Empty makes DMRHub use a certificate of its own, which plugins check by its fingerprint"`
	KeyFile  string `json:"key-file" yaml:"key-file" name:"key-file" description:"TLS key file of the certificate file"`
}

// Positions holds the limits of position tracking and the APRS-IS login
// of the hub.
type Positions struct {
	MinInterval  int    `json:"min-interval" yaml:"min-interval" name:"min-interval" description:"Shortest report interval, in minutes, that an owner can pick for a radio: 5, 10, 15, 30 or 60" default:"5"`
	MaxRetention int    `json:"max-retention" yaml:"max-retention" name:"max-retention" description:"Longest time, in days, that an owner can keep the positions of a radio, from 1 to 365" default:"30"`
	APRSIS       APRSIS `json:"aprs-is" yaml:"aprs-is" name:"aprs-is" description:"APRS-IS login of the hub, which sends the positions of the radios whose owners turn it on"`
}

const (
	defaultPositionInterval  = 5
	defaultPositionRetention = 30
	defaultAPRSServer        = "rotate.aprs2.net:14580"
)

// Interval returns the shortest report interval, in minutes; 0 means 5.
func (p Positions) Interval() int {
	if p.MinInterval == 0 {
		return defaultPositionInterval
	}
	return p.MinInterval
}

// Retention returns the longest position retention, in days; 0 means 30.
func (p Positions) Retention() int {
	if p.MaxRetention == 0 {
		return defaultPositionRetention
	}
	return p.MaxRetention
}

// ServerAddress returns the APRS-IS server; empty means the default.
func (a APRSIS) ServerAddress() string {
	if a.Server == "" {
		return defaultAPRSServer
	}
	return a.Server
}

// APRSIS holds the APRS-IS login of the hub.
type APRSIS struct {
	Callsign string `json:"callsign" name:"callsign" description:"Callsign with SSID that the hub logs in to APRS-IS as. Empty sends nothing to APRS-IS"`
	Passcode string `json:"-" yaml:"passcode" name:"passcode" description:"APRS-IS passcode of the callsign"`
	Server   string `json:"server" name:"server" description:"APRS-IS server, as host:port" default:"rotate.aprs2.net:14580"`
}

// Retention holds how many days DMRHub keeps each kind of record. 0 keeps
// the records forever.
type Retention struct {
	Messages      int `json:"messages" name:"messages" description:"Days to keep text and data messages, with their raw bytes. 0 keeps them forever" default:"0"`
	Transmissions int `json:"transmissions" name:"transmissions" description:"Days to keep the last heard entries of voice, data and control traffic. 0 keeps them forever" default:"0"`
	Bursts        int `json:"bursts" name:"bursts" description:"Days to keep the raw DMR bursts of each transmission. 0 keeps them forever" default:"0"`
}

// MMDVM holds the configuration for the MMDVM server.
type MMDVM struct {
	Bind string `json:"bind,omitempty" name:"bind" description:"MMDVM server listen address" default:"[::]"`
	Port int    `json:"port,omitempty" name:"port" description:"MMDVM server port" default:"62031"`
	// WebTransport is the listener of browser hotspots and radios, which
	// speak the MMDVM network protocol in WebTransport datagrams.
	WebTransport MMDVMWebTransport `json:"webtransport,omitempty" name:"webtransport" description:"WebTransport listener for browser hotspots and radios"`
}

// MMDVMWebTransport holds the configuration of the WebTransport listener of
// the MMDVM server. It listens on the bind address of the MMDVM server.
type MMDVMWebTransport struct {
	Enabled  bool   `json:"enabled,omitempty" name:"enabled" description:"Accept browser hotspots and radios over WebTransport" default:"false"`
	Port     int    `json:"port,omitempty" name:"port" description:"UDP port of the WebTransport listener" default:"62032"`
	CertFile string `json:"cert-file,omitempty" yaml:"cert-file" name:"cert-file" description:"TLS certificate file, such as the one that cert-manager mounts. Empty makes DMRHub use short-lived certificates of its own, which browsers accept through the hashes that DMRHub publishes"`
	KeyFile  string `json:"key-file,omitempty" yaml:"key-file" name:"key-file" description:"TLS key file of the certificate file"`
}

// IPSC holds the configuration for the IPSC server.
type IPSC struct {
	Enabled   bool   `json:"enabled,omitempty" name:"enabled" description:"Enable IPSC server support" default:"false"`
	Bind      string `json:"bind,omitempty" name:"bind" description:"IPSC server listen address" default:"[::]"`
	Port      int    `json:"port,omitempty" name:"port" description:"IPSC server port" default:"50000"`
	NetworkID uint32 `json:"network-id,omitempty" yaml:"network-id" name:"network-id" description:"DMR network ID that identifies this server to IPSC peers. Required when IPSC is enabled"`
}

// OpenBridge holds the configuration for the OpenBridge server.
type OpenBridge struct {
	Enabled bool   `json:"enabled,omitempty" name:"enabled" description:"Enable OpenBridge server support" default:"false"`
	Bind    string `json:"bind,omitempty" name:"bind" description:"OpenBridge server listen address" default:"[::]"`
	Port    int    `json:"port,omitempty" name:"port" description:"OpenBridge server port" default:"62035"`
	// ServerID is the source server that DMRHub names in the DMRE packets
	// that it sends to enhanced OpenBridge peers.
	ServerID uint32 `json:"server-id,omitempty" yaml:"server-id" name:"server-id" description:"Server ID of 4 to 7 digits that DMRHub sends to enhanced (FreeDMR) OpenBridge peers as the source server. 0 sends the network ID of each peer"`
}

// SMTP holds the SMTP configuration.
type SMTP struct {
	Enabled    bool           `json:"enabled,omitempty" name:"enabled" description:"Enable SMTP support for sending emails" default:"false"`
	Host       string         `json:"host,omitempty" name:"host" description:"SMTP server host address"`
	Port       int            `json:"port,omitempty" name:"port" description:"SMTP server port" default:"25"`
	TLS        SMTPTLS        `json:"tls,omitempty" name:"tls" description:"SMTP TLS mode. One of none, starttls, or implicit" default:"none"`
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
