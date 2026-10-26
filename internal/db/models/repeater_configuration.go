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

package models

import (
	"errors"
	"log/slog"
	"math"
	"runtime"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
)

//go:generate go run github.com/tinylib/msgp
type RepeaterConfiguration struct {
	Callsign    string  `json:"callsign" msg:"callsign"`
	ID          uint    `json:"id" gorm:"primaryKey" msg:"id"`
	RXFrequency uint    `json:"rx_frequency" msg:"rx_frequency"`
	TXFrequency uint    `json:"tx_frequency" msg:"tx_frequency"`
	TXPower     uint8   `json:"tx_power" msg:"tx_power"`
	ColorCode   uint8   `json:"color_code" msg:"color_code"`
	Latitude    float64 `json:"latitude" msg:"latitude"`
	Longitude   float64 `json:"longitude" msg:"longitude"`
	Height      uint16  `json:"height" msg:"height"`
	Location    string  `json:"location" msg:"location"`
	Description string  `json:"description" msg:"description"`
	URL         string  `json:"url" msg:"url"`
	SoftwareID  string  `json:"software_id" msg:"software_id"`
	PackageID   string  `json:"package_id" msg:"package_id"`
	Slots       uint    `json:"slots" msg:"slots"`
}

var ErrInvalidCallsign = errors.New("invalid callsign")

const (
	maxTXPower        = 99
	maxColorCode      = 15
	minLatitude       = -90
	maxLatitude       = 90
	minLongitude      = -180
	maxLongitude      = 180
	maxHeight         = 999
	locationMaxLen    = 20
	descriptionMaxLen = 20
	urlMaxLen         = 124
	softwareIDMaxLen  = 40
	packageIDMaxLen   = 40
)

var ErrInvalidConfig = errors.New("invalid RPTC configuration packet")

// ParseConfig reads the 302-octet RPTC configuration message data of the
// MMDVM network protocol.
//
// Clients pad the fields differently: DMRGateway and MMDVMHost zero fill the
// numbers, HBlink3 left-justifies frequencies and coordinates with spaces,
// and HBlink4 right-justifies coordinates with spaces. Every field is read
// with leading and trailing spaces and NULs removed. Only the callsign can
// refuse the login; any other field that does not parse, or a color code
// above 15, is logged and left at zero, as FreeDMR and HBlink store it as
// plain text.
// The numbers are read from their octet ranges in dmrgo's config layouts,
// since dmrgo's accessors refuse anything but digits.
func (c *RepeaterConfiguration) ParseConfig(data []byte, version, commit string) error {
	cfg, err := dmrd.MMDVMConfigOf(data)
	if err != nil {
		slog.Error("Error reading RPTC packet", "error", err)
		return ErrInvalidConfig
	}
	id := cfg.ID()
	c.Slots = uint(rptcNumber[uint8](id, "Slots", cfg[97:98]))
	return c.parse(id, cfg[:], 16, cfg, version, commit)
}

// ParseHBRPConfig reads the 306-octet RPTC configuration message data of
// HBRP, which has no slots field, as ParseConfig reads the MMDVM one.
func (c *RepeaterConfiguration) ParseHBRPConfig(data []byte, version, commit string) error {
	cfg, err := dmrd.HBRPConfigOf(data)
	if err != nil {
		slog.Error("Error reading RPTC packet", "error", err)
		return ErrInvalidConfig
	}
	id, err := cfg.ID()
	if err != nil {
		slog.Error("Error reading RPTC repeater ID", "error", err)
		return ErrInvalidConfig
	}
	c.Slots = 0
	return c.parse(id, cfg[:], 20, cfg, version, commit)
}

type rptcText interface {
	Callsign() []byte
	Location() []byte
	Description() []byte
	URL() []byte
	SoftwareID() []byte
	PackageID() []byte
}

// parse reads the fields that both dialects carry. The numeric fields are
// at the same offsets from freq, the first octet of the RX frequency.
func (c *RepeaterConfiguration) parse(id uint32, raw []byte, freq int, cfg rptcText, version, commit string) error {
	f := raw[freq:]
	c.Callsign = strings.ToUpper(rptcString(cfg.Callsign()))
	c.RXFrequency = uint(rptcNumber[uint32](id, "RXFrequency", f[0:9]))
	c.TXFrequency = uint(rptcNumber[uint32](id, "TXFrequency", f[9:18]))
	c.TXPower = rptcNumber[uint8](id, "Power", f[18:20])

	c.ColorCode = rptcNumber[uint8](id, "ColorCode", f[20:22])
	if c.ColorCode > maxColorCode {
		slog.Warn("Ignoring an RPTC color code above 15", "repeaterID", id, "value", string(f[20:22]))
		c.ColorCode = 0
	}

	c.Latitude = rptcCoordinate(id, "Latitude", f[22:30], maxLatitude)
	c.Longitude = rptcCoordinate(id, "Longitude", f[30:39], maxLongitude)
	c.Height = rptcNumber[uint16](id, "Height", f[39:42])

	c.Location = rptcString(cfg.Location())
	c.Description = rptcString(cfg.Description())
	c.URL = rptcString(cfg.URL())

	c.SoftwareID = rptcString(cfg.SoftwareID())
	if c.SoftwareID == "" {
		c.SoftwareID = "USA-RedDragon/DMRHub " + version + "-" + commit
	}

	c.PackageID = rptcString(cfg.PackageID())
	if c.PackageID == "" {
		c.PackageID = version + "-" + commit
	}

	return c.Check(version, commit)
}

func rptcString(b []byte) string {
	return strings.Trim(string(b), " \x00")
}

func rptcDecimal[T uint8 | uint16 | uint32](b []byte) (T, bool) {
	s := rptcString(b)
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil || v > uint64(^T(0)) {
		return 0, false
	}
	return T(v), true
}

func rptcNumber[T uint8 | uint16 | uint32](id uint32, field string, raw []byte) T {
	v, ok := rptcDecimal[T](raw)
	if !ok {
		slog.Warn("Ignoring unparsable RPTC field", "repeaterID", id, "field", field, "value", string(raw))
	}
	return v
}

func rptcCoordinate(id uint32, field string, raw []byte, limit float64) float64 {
	s := rptcString(raw)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil || math.IsNaN(v) || v < -limit || v > limit {
		slog.Warn("Ignoring unparsable RPTC field", "repeaterID", id, "field", field, "value", string(raw))
		return 0
	}
	return v
}

func (c *RepeaterConfiguration) Check(version, commit string) error {
	if len(c.Callsign) < 4 || len(c.Callsign) > 8 {
		return ErrInvalidCallsign
	}
	c.Callsign = strings.ToUpper(c.Callsign)
	if !dmrconst.CallsignRegex.MatchString(c.Callsign) {
		return ErrInvalidCallsign
	}

	if c.TXPower > maxTXPower {
		c.TXPower = maxTXPower
	}

	if c.ColorCode > maxColorCode {
		c.ColorCode = 0
	}

	if math.IsNaN(c.Latitude) || c.Latitude < minLatitude || c.Latitude > maxLatitude {
		c.Latitude = 0
	}

	if math.IsNaN(c.Longitude) || c.Longitude < minLongitude || c.Longitude > maxLongitude {
		c.Longitude = 0
	}

	if c.Height > maxHeight {
		c.Height = maxHeight
	}

	if len(c.Location) > locationMaxLen {
		c.Location = c.Location[:locationMaxLen]
	}

	if len(c.Description) > descriptionMaxLen {
		c.Description = c.Description[:descriptionMaxLen]
	}

	if len(c.URL) > urlMaxLen {
		c.URL = c.URL[:urlMaxLen]
	}

	if len(c.SoftwareID) > softwareIDMaxLen {
		c.SoftwareID = c.SoftwareID[:softwareIDMaxLen]
	} else if c.SoftwareID == "" {
		c.SoftwareID = "USA-RedDragon/DMRHub v" + version + "-" + commit
	}

	if len(c.PackageID) > packageIDMaxLen {
		c.PackageID = c.PackageID[:packageIDMaxLen]
	} else if c.PackageID == "" {
		c.PackageID = runtime.Version()
	}

	return nil
}
