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
	"net/url"
	"strings"
	"time"

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
	AutoApprove  bool     `json:"auto-approve-users,omitempty" yaml:"auto-approve-users" name:"auto-approve-users" description:"Approve new users when they register, so they can log in at once without waiting for an admin"`
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
	ExtraParameters []string       `json:"extra-parameters,omitempty" yaml:"extra-parameters" name:"extra-parameters" description:"Additional parameters for the database connection, e.g., sslmode=disable" default:"_pragma=foreign_keys(1),_pragma=journal_mode(WAL),_pragma=synchronous(NORMAL)"`
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
	MMDVM                    MMDVM       `json:"mmdvm,omitempty" name:"mmdvm" description:"MMDVM server configuration for DMR"`
	OpenBridge               OpenBridge  `json:"openbridge,omitempty" name:"openbridge" description:"OpenBridge server configuration for DMR"`
	IPSC                     IPSC        `json:"ipsc,omitempty" name:"ipsc" description:"IPSC server configuration for Motorola DMR repeaters"`
	Modem                    Modem       `json:"modem" name:"modem" description:"MMDVM modem board on a serial port that DMRHub drives as a hotspot or repeater of its own, without MMDVMHost"`
	DisableRadioIDValidation bool        `json:"disable-radio-id-validation,omitempty" name:"disable-radio-id-validation" description:"Disable validation of radio IDs in DMR packets, allowing any 7- to 9-digit number to be used as a radio ID" default:"false"`
	RadioIDURL               string      `json:"radio-id-url,omitempty" name:"radio-id-url" description:"URL to fetch radio ID information for validation and display purposes. Expected JSON format is the same as RadioID.net." default:"https://www.radioid.net/static/users.json"`
	RepeaterIDURL            string      `json:"repeater-id-url,omitempty" name:"repeater-id-url" description:"URL to fetch repeater information for validation and display purposes. Expected JSON format is the same as RadioID.net." default:"https://www.radioid.net/static/rptrs.json"`
	Retention                Retention   `json:"retention" name:"retention" description:"How long DMRHub keeps its records"`
	Positions                Positions   `json:"positions" name:"positions" description:"Position tracking of the radios whose owners turn it on"`
	Plugins                  Plugins     `json:"plugins" name:"plugins" description:"Plugins, which answer the calls to the IDs an admin gives them: inside DMRHub, or as their own programs that connect to its listener"`
	Transcoding              Transcoding `json:"transcoding" name:"transcoding" description:"Conversion of calls between Codec2 and AMBE"`
	DrainTimeout             int         `json:"drain-timeout" yaml:"drain-timeout" name:"drain-timeout" description:"Longest time, in seconds, that a replica that stops keeps serving its repeaters, peers, browser radios and plugins while other replicas take them over. It must be shorter than the time the orchestrator gives the replica to stop. 0 stops at once" default:"25"`
}

// Transcoding holds the AMBE servers that convert calls between Codec2 and
// AMBE.
type Transcoding struct {
	AMBEServers []AMBEServer `json:"ambe-servers" yaml:"ambe-servers,omitempty" name:"ambe-servers" description:"AMBE servers (AMBEserver protocol, usually UDP port 2460) that convert calls between Codec2 and AMBE, in the order a call tries them. Each converted call holds one server for its whole length. None turns transcoding off, and calls pass through in their own codec. Each server must accept the setting that makes it report the decoder status of each frame (PKT_SPCHFMT); a server that refuses it converts no call"`
}

// AMBEServer is one AMBE server.
type AMBEServer struct {
	Address string `json:"address" yaml:"address" name:"address" description:"Host and UDP port of the AMBE server, such as ambeserver:2460"`
	Calls   int    `json:"calls" yaml:"calls" name:"calls" description:"Calls the server converts at once. 0 is no limit; use 1 for an AMBEserver with one DVSI chip, which answers only its last client"`
}

// Plugins holds where the plugins run: inside DMRHub, and the listener that
// the plugins that run as their own programs connect to.
type Plugins struct {
	InProcess bool   `json:"in-process" yaml:"in-process" name:"in-process" description:"Run the parrot, the location server and the uploaded plugins inside DMRHub. Turn it off only when every plugin runs as its own program" default:"true"`
	Enabled   bool   `json:"enabled" name:"enabled" description:"Accept plugins that run as their own programs" default:"false"`
	Bind      string `json:"bind" name:"bind" description:"Address the plugin listener binds to" default:"[::]"`
	Port      int    `json:"port" name:"port" description:"TCP port that plugins connect to" default:"3006"`
	CertFile  string `json:"cert-file" yaml:"cert-file" name:"cert-file" description:"TLS certificate file of the plugin listener. Empty makes DMRHub use a certificate of its own, which plugins check by its fingerprint"`
	KeyFile   string `json:"key-file" yaml:"key-file" name:"key-file" description:"TLS key file of the certificate file"`
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

// APRSIS holds the APRS-IS login of the hub. Its passcode comes from the
// callsign.
type APRSIS struct {
	Callsign string `json:"callsign" name:"callsign" description:"Callsign with SSID that the hub logs in to APRS-IS as. Empty sends nothing to APRS-IS"`
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
	// AllowedOrigins are the origins of the web pages that may open a
	// session, in the form scheme://host[:port]. Empty allows every origin.
	AllowedOrigins []string `json:"allowed-origins,omitempty" yaml:"allowed-origins" name:"allowed-origins" description:"Origins of the web pages that may open a session, such as https://radio.example. Empty allows every origin"`
}

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// NormalizeOrigin returns the origin s in the form that browsers send in
// the Origin header: the scheme and host in lower case, and the port only
// when it is not the default port of the scheme. ok is false when s is not
// an http or https origin, or has a path other than "/", a query, a
// fragment or user information.
func NormalizeOrigin(s string) (origin string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery ||
		(u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != schemeHTTP && scheme != schemeHTTPS {
		return "", false
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if host == "" {
		return "", false
	}
	if (scheme == schemeHTTPS && port == "443") || (scheme == schemeHTTP && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		return scheme + "://" + host + ":" + port, true
	}
	return scheme + "://" + host, true
}

// IPSC holds the configuration for the IPSC server.
type IPSC struct {
	Enabled   bool   `json:"enabled,omitempty" name:"enabled" description:"Enable IPSC server support" default:"false"`
	Bind      string `json:"bind,omitempty" name:"bind" description:"IPSC server listen address" default:"[::]"`
	Port      int    `json:"port,omitempty" name:"port" description:"IPSC server port" default:"50000"`
	NetworkID uint32 `json:"network-id,omitempty" yaml:"network-id" name:"network-id" description:"DMR network ID that identifies this server to IPSC peers. Required when IPSC is enabled"`
	// CallHangHold is how long after a voice call to or from an IPSC peer ends on
	// a slot that DMRHub holds the data and CSBK calls to that slot.
	CallHangHold time.Duration `json:"call-hang-hold,omitempty" yaml:"call-hang-hold" name:"call-hang-hold" description:"Wait for data and CSBK calls to IPSC after a voice call on a slot; 0 is off" default:"2.5s"`
	// DataHangHold is how long after a data call to or from an IPSC peer ends on
	// a slot that DMRHub holds the data and CSBK calls to that slot.
	DataHangHold time.Duration `json:"data-hang-hold,omitempty" yaml:"data-hang-hold" name:"data-hang-hold" description:"Wait for data and CSBK calls to IPSC after a data call on a slot; 0 is off" default:"1.2s"`
}

// MaxCallHangHold is the longest dmr.ipsc.call-hang-hold or
// dmr.ipsc.data-hang-hold.
const MaxCallHangHold = 10 * time.Second

// Modem holds the MMDVM modem board that DMRHub drives over a serial port
// as one of its repeaters.
type Modem struct {
	Enabled         bool         `json:"enabled" name:"enabled" description:"Drive an MMDVM modem board (MMDVM_HS hat, MMDVM repeater board, DVMega) on a serial port as a hotspot or repeater of this server" default:"false"`
	Port            string       `json:"port" name:"port" description:"Serial device of the modem, such as /dev/ttyAMA0 or /dev/ttyACM0. Required when the modem is enabled"`
	Baud            int          `json:"baud" name:"baud" description:"Speed of the serial port in baud: 9600, 19200, 38400, 57600, 115200, 230400, 460800 or 921600" default:"115200"`
	RepeaterID      uint         `json:"repeater-id" yaml:"repeater-id" name:"repeater-id" description:"ID of the repeater that the modem is on the network. DMRHub makes it a modem repeater when the modem connects, and refuses network logins with its ID. A repeater that does not exist is made with no owner, and an admin assigns its owner. Required when the modem is enabled"`
	Duplex          bool         `json:"duplex" name:"duplex" description:"Run the modem as a duplex repeater with two timeslots. Off runs it as a simplex hotspot, whose one channel is timeslot 2" default:"false"`
	ColorCode       uint         `json:"color-code" yaml:"color-code" name:"color-code" description:"Color code of the modem, 0 to 15" default:"1"`
	RXFrequency     uint         `json:"rx-frequency" yaml:"rx-frequency" name:"rx-frequency" description:"Receive frequency in Hz. Required when the modem is enabled"`
	TXFrequency     uint         `json:"tx-frequency" yaml:"tx-frequency" name:"tx-frequency" description:"Transmit frequency in Hz. Required when the modem is enabled"`
	RXOffset        int          `json:"rx-offset" yaml:"rx-offset" name:"rx-offset" description:"Correction in Hz added to the receive frequency, for the frequency error of the board" default:"0"`
	TXOffset        int          `json:"tx-offset" yaml:"tx-offset" name:"tx-offset" description:"Correction in Hz added to the transmit frequency, for the frequency error of the board" default:"0"`
	RFLevel         float64      `json:"rf-level" yaml:"rf-level" name:"rf-level" description:"Transmit power in percent, on boards that set it, such as MMDVM_HS hats" default:"100"`
	RXLevel         float64      `json:"rx-level" yaml:"rx-level" name:"rx-level" description:"Receive audio level in percent" default:"50"`
	TXLevel         float64      `json:"tx-level" yaml:"tx-level" name:"tx-level" description:"Transmit deviation level in percent" default:"50"`
	RXDCOffset      int          `json:"rx-dc-offset" yaml:"rx-dc-offset" name:"rx-dc-offset" description:"Receive DC offset, -128 to 127" default:"0"`
	TXDCOffset      int          `json:"tx-dc-offset" yaml:"tx-dc-offset" name:"tx-dc-offset" description:"Transmit DC offset, -128 to 127" default:"0"`
	RXInvert        bool         `json:"rx-invert" yaml:"rx-invert" name:"rx-invert" description:"Invert the receive audio" default:"false"`
	TXInvert        bool         `json:"tx-invert" yaml:"tx-invert" name:"tx-invert" description:"Invert the transmit audio, which most boards need" default:"true"`
	PTTInvert       bool         `json:"ptt-invert" yaml:"ptt-invert" name:"ptt-invert" description:"Invert the PTT output" default:"false"`
	TXDelay         int          `json:"tx-delay" yaml:"tx-delay" name:"tx-delay" description:"Time in milliseconds, 0 to 500, that the transmitter is on before the first burst" default:"100"`
	DMRDelay        int          `json:"dmr-delay" yaml:"dmr-delay" name:"dmr-delay" description:"Delay of the duplex receiver in symbols, 0 to 255" default:"0"`
	Debug           bool         `json:"debug" name:"debug" description:"Ask the modem for its debug messages and log them" default:"false"`
	RSSIMappingFile string       `json:"rssi-mapping-file" yaml:"rssi-mapping-file" name:"rssi-mapping-file" description:"Path to an RSSI.dat file in the MMDVMHost format, which maps the raw RSSI of the board to dBm. Without it the RSSI of modem calls is not known"`
	CWID            ModemCWID    `json:"cw-id" yaml:"cw-id" name:"cw-id" description:"Morse station ID that the modem sends while the channel is idle"`
	Beacons         ModemBeacons `json:"beacons" name:"beacons" description:"Roaming beacons of a duplex modem, as MMDVMHost sends them"`
}

// ModemBeacons are the timed roaming beacons of a duplex modem, as the DMR
// beacons of MMDVMHost: the transmitter keys up for a time at each
// interval while the channel is idle, so that radios can find the
// repeater.
type ModemBeacons struct {
	Enabled  bool `json:"enabled" name:"enabled" description:"Key up the transmitter of a duplex modem at each interval while the channel is idle" default:"false"`
	Interval uint `json:"interval" name:"interval" description:"Seconds between beacons, 1 to 86400" default:"60"`
	Duration uint `json:"duration" name:"duration" description:"Seconds that each beacon keys up, at least 1 and less than the interval" default:"3"`
}

// ModemCWID is the morse station ID of the modem, as the CW Id section of
// MMDVMHost.
type ModemCWID struct {
	Enabled  bool   `json:"enabled" name:"enabled" description:"Send a morse station ID while the channel is idle" default:"false"`
	Time     uint   `json:"time" name:"time" description:"Minutes between station IDs, 1 to 1440. The first goes out a quarter of this time after the modem connects or the channel goes idle" default:"10"`
	Callsign string `json:"callsign" name:"callsign" description:"Callsign to send in morse: letters, digits, spaces and / ? , - = . Required when the station ID is on"`
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
