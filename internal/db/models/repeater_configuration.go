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

func (c *RepeaterConfiguration) ParseConfig(data []byte, version, commit string) error {
	c.Callsign = strings.ToUpper(strings.TrimRight(string(data[8:16]), " "))

	rxFreq, err := strconv.ParseInt(strings.TrimRight(string(data[16:25]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing rx frequency", "error", err)
		return ErrInvalidInt
	}
	c.RXFrequency = uint(rxFreq)

	txFreq, err := strconv.ParseInt(strings.TrimRight(string(data[25:34]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing tx frequency", "error", err)
		return ErrInvalidInt
	}
	c.TXFrequency = uint(txFreq)

	txPower, err := strconv.ParseInt(strings.TrimRight(string(data[34:36]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing tx power", "error", err)
		return ErrInvalidInt
	}
	c.TXPower = uint8(txPower)

	colorCode, err := strconv.ParseInt(strings.TrimRight(string(data[36:38]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing color code", "error", err)
		return ErrInvalidInt
	}
	c.ColorCode = uint8(colorCode)

	lat, err := strconv.ParseFloat(strings.TrimRight(string(data[38:46]), " "), 32)
	if err != nil {
		slog.Error("Error parsing latitude", "error", err)
		return ErrInvalidFloat
	}
	c.Latitude = lat

	long, err := strconv.ParseFloat(strings.TrimRight(string(data[46:55]), " "), 32)
	if err != nil {
		slog.Error("Error parsing longitude", "error", err)
		return ErrInvalidFloat
	}
	c.Longitude = long

	height, err := strconv.ParseInt(strings.TrimRight(string(data[55:58]), " "), 0, 32)
	if err != nil {
		slog.Error("Error parsing height", "error", err)
		return ErrInvalidInt
	}
	c.Height = uint16(height)

	c.Location = strings.TrimRight(string(data[58:78]), " ")

	c.Description = strings.TrimRight(string(data[78:97]), " ")

	slots, err := strconv.ParseInt(string(data[97]), 0, 32)
	if err != nil {
		slog.Error("Error parsing slots", "error", err)
		return ErrInvalidInt
	}
	c.Slots = uint(slots)

	c.URL = strings.TrimRight(string(data[98:222]), " ")

	c.SoftwareID = strings.TrimRight(string(data[222:262]), " ")
	if c.SoftwareID == "" {
		c.SoftwareID = "USA-RedDragon/DMRHub " + version + "-" + commit
	}

	c.PackageID = strings.TrimRight(string(data[262:302]), " ")
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
