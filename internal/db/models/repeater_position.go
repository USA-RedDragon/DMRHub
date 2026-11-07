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
	"fmt"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"gorm.io/gorm"
)

// RepeaterOnline is how long after its last ping an MMDVM repeater counts
// as connected: the session timeout of the MMDVM network protocol.
const RepeaterOnline = dmrdsession.TimeoutDefault

// ConnectedRepeaterPositions returns the MMDVM repeaters that pinged in the
// RepeaterOnline before now and reported a position, ordered by ID. A
// repeater reports no position when its RPTC carries 0 for both latitude
// and longitude, as DMRGateway sends it for a network with Location=0.
// With all false it returns only the repeaters that userID owns or is a
// co-sysop of.
func ConnectedRepeaterPositions(db *gorm.DB, userID uint, all bool, now time.Time) ([]Repeater, error) {
	q := db.Model(&Repeater{}).
		Where("type = ? AND last_ping > ? AND (latitude <> 0 OR longitude <> 0)", RepeaterTypeMMDVM, now.Add(-RepeaterOnline))
	if !all {
		q = q.Where("(owner_id = ? OR "+cosysopOf+")", userID, userID)
	}
	var repeaters []Repeater
	if err := q.Order("id asc").Find(&repeaters).Error; err != nil {
		return nil, fmt.Errorf("could not list the positions of the connected repeaters: %w", err)
	}
	return repeaters, nil
}
