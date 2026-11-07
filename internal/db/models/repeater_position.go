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

// Online reports whether the repeater has a session at now: it pinged in
// the RepeaterOnline before now, and Disconnected, when its last session
// ended by RPTCL or by timeout, is unset or before that ping.
// The repeater page shows the same rule.
func (p *Repeater) Online(now time.Time) bool {
	if p.LastPing.IsZero() || now.Sub(p.LastPing) >= RepeaterOnline {
		return false
	}
	return p.Disconnected == nil || p.LastPing.After(*p.Disconnected)
}

// ConnectedRepeaterPositions returns the MMDVM repeaters that are Online
// at now and reported a position, ordered by ID. A repeater reports no
// position when its RPTC carries 0 for both latitude and longitude, as
// DMRGateway sends it for a network with Location=0. With all false it
// returns only the repeaters that userID owns or is a co-sysop of.
func ConnectedRepeaterPositions(db *gorm.DB, userID uint, all bool, now time.Time) ([]Repeater, error) {
	q := db.Model(&Repeater{}).Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").
		Where("type = ? AND last_ping > ? AND (latitude <> 0 OR longitude <> 0)", RepeaterTypeMMDVM, now.Add(-RepeaterOnline))
	if !all {
		q = q.Where("(owner_id = ? OR "+cosysopOf+")", userID, userID)
	}
	var found []Repeater
	if err := q.Order("id asc").Find(&found).Error; err != nil {
		return nil, fmt.Errorf("could not list the positions of the connected repeaters: %w", err)
	}
	online := found[:0]
	for i := range found {
		if found[i].Online(now) {
			online = append(online, found[i])
		}
	}
	return online, nil
}

// SetRepeaterDisconnected records that the session of the repeater id
// ended at at.
func SetRepeaterDisconnected(db *gorm.DB, id uint, at time.Time) error {
	if err := db.Model(&Repeater{}).Where("id = ?", id).UpdateColumn("disconnected", at).Error; err != nil {
		return fmt.Errorf("could not record the disconnect of repeater %d: %w", id, err)
	}
	return nil
}
