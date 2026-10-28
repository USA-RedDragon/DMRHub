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

	"gorm.io/gorm"
)

// ErrRepeaterHasOwner indicates an owner assignment for a repeater that
// has one.
var ErrRepeaterHasOwner = errors.New("the repeater has an owner")

// unassigned selects the repeaters with no owner. Such a repeater keeps
// owner_id NULL, so that the foreign key to users holds.
const unassigned = "owner_id IS NULL OR owner_id = 0"

// ListUnassignedRepeaters returns the repeaters with no owner, such as the
// one that DMRHub makes for a modem whose repeater did not exist. An admin
// assigns their owner with AssignRepeaterOwner.
func ListUnassignedRepeaters(db *gorm.DB) ([]Repeater, error) {
	var repeaters []Repeater
	err := db.Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").
		Where(unassigned).Order("id asc").Find(&repeaters).Error
	return repeaters, err
}

// CountUnassignedRepeaters returns the number of repeaters with no owner.
func CountUnassignedRepeaters(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&Repeater{}).Where(unassigned).Count(&count).Error
	return int(count), err
}

// AssignRepeaterOwner makes the user owner the owner of the repeater id,
// which has none. It returns gorm.ErrRecordNotFound when the repeater does
// not exist and ErrRepeaterHasOwner when it has an owner.
func AssignRepeaterOwner(db *gorm.DB, id, owner uint) error {
	res := db.Model(&Repeater{}).Where("id = ?", id).Where(unassigned).UpdateColumn("owner_id", owner)
	if res.Error != nil {
		return fmt.Errorf("assign the owner of repeater %d: %w", id, res.Error)
	}
	if res.RowsAffected > 0 {
		return nil
	}
	exists, err := RepeaterIDExists(db, id)
	switch {
	case err != nil:
		return fmt.Errorf("find repeater %d: %w", id, err)
	case exists:
		return ErrRepeaterHasOwner
	}
	return gorm.ErrRecordNotFound
}

// BeforeSave leaves out owner_id for a repeater with no owner, so that it
// stays NULL and the foreign key to users holds.
func (p *Repeater) BeforeSave(tx *gorm.DB) error {
	if p.OwnerID == 0 {
		tx.Statement.Omits = append(tx.Statement.Omits, "owner_id")
	}
	return nil
}

// RepeaterServes reports whether the radios of userID may use the repeater
// repeaterID for private traffic and positions: the user owns the repeater
// or an admin trusts it. It returns the repeater it found, with ID 0 when
// there is none.
func RepeaterServes(db *gorm.DB, userID, repeaterID uint) (Repeater, bool, error) {
	var rpt Repeater
	err := db.Select("id", "owner_id", "trusted", "color_code").First(&rpt, repeaterID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Repeater{}, false, nil
	}
	if err != nil {
		return Repeater{}, false, fmt.Errorf("finding repeater %d: %w", repeaterID, err)
	}
	return rpt, rpt.OwnerID == userID || rpt.Trusted, nil
}
