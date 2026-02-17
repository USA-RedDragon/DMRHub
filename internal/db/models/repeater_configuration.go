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
	"runtime"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
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

var (
	ErrInvalidCallsign  = errors.New("invalid callsign")
	ErrInvalidColorCode = errors.New("invalid color code")
	ErrInvalidLatitude  = errors.New("invalid latitude")
	ErrInvalidLongitude = errors.New("invalid longitude")
	ErrInvalidInt       = errors.New("invalid integer")
	ErrInvalidFloat     = errors.New("invalid float")
)

const (
	maxTXPower        = 99
	minColorCode      = 1
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

// Config packet field byte offsets.
// Layout: [RPTC(4)][ID(4)][Callsign(8)][RXFreq(9)][TXFreq(9)][TXPower(2)]
//
//	[ColorCode(2)][Lat(8)][Lon(9)][Height(3)][Location(20)][Desc(19)]
//	[Slots(1)][URL(124)][SoftwareID(40)][PackageID(40)] = 302 bytes
const (
	cfgCallsignStart    = 8
	cfgCallsignEnd      = 16
	cfgRXFreqStart      = 16
	cfgRXFreqEnd        = 25
	cfgTXFreqStart      = 25
	cfgTXFreqEnd        = 34
	cfgTXPowerStart     = 34
	cfgTXPowerEnd       = 36
	cfgColorCodeStart   = 36
	cfgColorCodeEnd     = 38
	cfgLatitudeStart    = 38
	cfgLatitudeEnd      = 46
	cfgLongitudeStart   = 46
	cfgLongitudeEnd     = 55
	cfgHeightStart      = 55
	cfgHeightEnd        = 58
	cfgLocationStart    = 58
	cfgLocationEnd      = 78
	cfgDescriptionStart = 78
	cfgDescriptionEnd   = 97
	cfgSlotsOffset      = 97
	cfgURLStart         = 98
	cfgURLEnd           = 222
	cfgSoftwareIDStart  = 222
	cfgSoftwareIDEnd    = 262
	cfgPackageIDStart   = 262
	cfgPackageIDEnd     = 302
)

func (c *RepeaterConfiguration) ParseConfig(data []byte, version, commit string) error {
	c.Callsign = strings.ToUpper(strings.TrimRight(string(data[cfgCallsignStart:cfgCallsignEnd]), " "))

	rxFreq, err := strconv.ParseInt(strings.TrimRight(string(data[cfgRXFreqStart:cfgRXFreqEnd]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing rx frequency", "error", err)
		return ErrInvalidInt
	}
	if rxFreq < 0 {
		slog.Error("Invalid rx frequency: negative value", "frequency", rxFreq)
		return ErrInvalidInt
	}
	c.RXFrequency = uint(rxFreq)

	txFreq, err := strconv.ParseInt(strings.TrimRight(string(data[cfgTXFreqStart:cfgTXFreqEnd]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing tx frequency", "error", err)
		return ErrInvalidInt
	}
	if txFreq < 0 {
		slog.Error("Invalid tx frequency: negative value", "frequency", txFreq)
		return ErrInvalidInt
	}
	c.TXFrequency = uint(txFreq)

	txPower, err := strconv.ParseInt(strings.TrimRight(string(data[cfgTXPowerStart:cfgTXPowerEnd]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing tx power", "error", err)
		return ErrInvalidInt
	}
	if txPower < 0 || txPower > 255 {
		slog.Error("Invalid tx power: out of range for uint8", "power", txPower)
		return ErrInvalidInt
	}
	c.TXPower = uint8(txPower)

	colorCode, err := strconv.ParseInt(strings.TrimRight(string(data[cfgColorCodeStart:cfgColorCodeEnd]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing color code", "error", err)
		return ErrInvalidInt
	}
	if colorCode < 0 || colorCode > 15 {
		slog.Error("Invalid color code: out of range for uint8", "colorCode", colorCode)
		return ErrInvalidInt
	}
	c.ColorCode = uint8(colorCode)

	lat, err := strconv.ParseFloat(strings.TrimRight(string(data[cfgLatitudeStart:cfgLatitudeEnd]), " "), 32)
	if err != nil {
		slog.Error("Error parsing latitude", "error", err)
		return ErrInvalidFloat
	}
	c.Latitude = lat

	long, err := strconv.ParseFloat(strings.TrimRight(string(data[cfgLongitudeStart:cfgLongitudeEnd]), " "), 32)
	if err != nil {
		slog.Error("Error parsing longitude", "error", err)
		return ErrInvalidFloat
	}
	c.Longitude = long

	height, err := strconv.ParseInt(strings.TrimRight(string(data[cfgHeightStart:cfgHeightEnd]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing height", "error", err)
		return ErrInvalidInt
	}
	if height < 0 || height > 65535 {
		slog.Error("Invalid height: out of range for uint16", "height", height)
		return ErrInvalidInt
	}
	c.Height = uint16(height)

	c.Location = strings.TrimRight(string(data[cfgLocationStart:cfgLocationEnd]), " ")

	c.Description = strings.TrimRight(string(data[cfgDescriptionStart:cfgDescriptionEnd]), " ")

	slots, err := strconv.ParseInt(string(data[cfgSlotsOffset]), 0, 32)
	if err != nil {
		slog.Error("Error parsing slots", "error", err)
		return ErrInvalidInt
	}
	if slots < 0 {
		slog.Error("Invalid slots: negative value", "slots", slots)
		return ErrInvalidInt
	}
	c.Slots = uint(slots)

	c.URL = strings.TrimRight(string(data[cfgURLStart:cfgURLEnd]), " ")

	c.SoftwareID = strings.TrimRight(string(data[cfgSoftwareIDStart:cfgSoftwareIDEnd]), " ")
	if c.SoftwareID == "" {
		c.SoftwareID = "USA-RedDragon/DMRHub " + version + "-" + commit
	}

	c.PackageID = strings.TrimRight(string(data[cfgPackageIDStart:cfgPackageIDEnd]), " ")
	if c.PackageID == "" {
		c.PackageID = version + "-" + commit
	}

	return c.Check(version, commit)
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

	if c.ColorCode < minColorCode || c.ColorCode > maxColorCode {
		return ErrInvalidColorCode
	}

	if c.Latitude < minLatitude || c.Latitude > maxLatitude {
		return ErrInvalidLatitude
	}

	if c.Longitude < minLongitude || c.Longitude > maxLongitude {
		return ErrInvalidLongitude
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
