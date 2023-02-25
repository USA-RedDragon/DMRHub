// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"runtime"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/sdk"
)

//go:generate msgp
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
)

const (
	MaxTXPower     = 99
	MaxHeight      = 999
	LenLocation    = 20
	LenDescription = 20
	LenURL         = 124
	LenSoftwareID  = 40
	LenPackageID   = 40
)

func (c *RepeaterConfiguration) Check() error {
	if len(c.Callsign) < 4 || len(c.Callsign) > 8 {
		return ErrInvalidCallsign
	}
	c.Callsign = strings.ToUpper(c.Callsign)
	if !dmrconst.CallsignRegex.MatchString(c.Callsign) {
		return ErrInvalidCallsign
	}

	if c.TXPower > MaxTXPower {
		c.TXPower = MaxTXPower
	}

	if c.ColorCode < 1 || c.ColorCode > 15 {
		return ErrInvalidColorCode
	}

	if c.Latitude < -90 || c.Latitude > 90 {
		return ErrInvalidLatitude
	}

	if c.Longitude < -180 || c.Longitude > 180 {
		return ErrInvalidLongitude
	}

	if c.Height > MaxHeight {
		c.Height = MaxHeight
	}

	if len(c.Location) > LenLocation {
		c.Location = c.Location[:LenLocation]
	}

	if len(c.Description) > LenDescription {
		c.Description = c.Description[:LenDescription]
	}

	if len(c.URL) > LenURL {
		c.URL = c.URL[:LenURL]
	}

	if len(c.SoftwareID) > LenSoftwareID {
		c.SoftwareID = c.SoftwareID[:LenSoftwareID]
	} else if c.SoftwareID == "" {
		c.SoftwareID = "USA-RedDragon/DMRHub v" + sdk.Version + "-" + sdk.GitCommit
	}

	if len(c.PackageID) > LenPackageID {
		c.PackageID = c.PackageID[:LenPackageID]
	} else if c.PackageID == "" {
		c.PackageID = runtime.Version()
	}

	return nil
}
