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
	"fmt"
	"math"
	"strconv"

	"gorm.io/gorm"
)

// ErrInvalidHomePosition is the error of a home position that does not
// parse.
var ErrInvalidHomePosition = errors.New("invalid RPTG home position")

// ParseHomePosition reads the text of an RPTG, the home position that
// DMRGateway sends from gpsd for a network with Location=1. DMRGateway
// writes it with the format "%+08.4f%+09.4f", as "+30.2672-097.7431".
func ParseHomePosition(text []byte) (float64, float64, error) {
	s := rptcString(text)
	split := -1
	for i := 1; i < len(s); i++ {
		if s[i] == '+' || s[i] == '-' {
			split = i
			break
		}
	}
	if split < 0 {
		return 0, 0, ErrInvalidHomePosition
	}
	lat, err := strconv.ParseFloat(s[:split], 64)
	if err != nil || math.IsNaN(lat) || lat < minLatitude || lat > maxLatitude {
		return 0, 0, ErrInvalidHomePosition
	}
	lon, err := strconv.ParseFloat(s[split:], 64)
	if err != nil || math.IsNaN(lon) || lon < minLongitude || lon > maxLongitude {
		return 0, 0, ErrInvalidHomePosition
	}
	return lat, lon, nil
}

// SetRepeaterPosition stores the latitude and longitude of the repeater id.
func SetRepeaterPosition(db *gorm.DB, id uint, lat, lon float64) error {
	err := db.Model(&Repeater{}).Where("id = ?", id).UpdateColumns(map[string]any{"latitude": lat, "longitude": lon}).Error
	if err != nil {
		return fmt.Errorf("could not store the position of repeater %d: %w", id, err)
	}
	return nil
}
