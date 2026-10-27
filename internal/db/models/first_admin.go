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
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FirstAdminID is the ID of the one FirstAdmin row.
const FirstAdminID = 1

// ErrFirstAdminClaimed is the error of ClaimFirstAdmin when a super admin
// exists already, or another visitor made the first admin first.
var ErrFirstAdminClaimed = errors.New("the first admin exists already")

// FirstAdmin records that the first admin was made. Its primary key holds
// only FirstAdminID, so the database lets one visitor make the first
// admin, on every replica and for the life of the database.
type FirstAdmin struct {
	ID        uint `gorm:"primaryKey;autoIncrement:false"`
	UserID    uint `gorm:"not null"`
	CreatedAt time.Time
}

func (FirstAdmin) TableName() string {
	return "first_admins"
}

// FirstAdminOpen reports whether a visitor can still make the first admin:
// no one made it yet and no super admin exists.
func FirstAdminOpen(db *gorm.DB) (bool, error) {
	var claims, supers int64
	if err := db.Model(&FirstAdmin{}).Count(&claims).Error; err != nil {
		return false, fmt.Errorf("could not count the first admins: %w", err)
	}
	if err := db.Model(&User{}).Where("super_admin = ?", true).Count(&supers).Error; err != nil {
		return false, fmt.Errorf("could not count the super admins: %w", err)
	}
	return claims == 0 && supers == 0, nil
}

// ClaimFirstAdmin makes user the first admin, approved and a super admin,
// in one transaction. The transaction first inserts the FirstAdmin row, so
// of two visitors at once, on any replica, the database lets only one
// through and the other gets ErrFirstAdminClaimed. It also returns
// ErrFirstAdminClaimed when a super admin exists already.
func ClaimFirstAdmin(db *gorm.DB, user *User) error {
	user.Approved, user.Admin, user.SuperAdmin = true, true, true
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&FirstAdmin{ID: FirstAdminID, UserID: user.ID})
		if res.Error != nil {
			return fmt.Errorf("could not claim the first admin: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return ErrFirstAdminClaimed
		}
		var supers int64
		if err := tx.Model(&User{}).Where("super_admin = ?", true).Count(&supers).Error; err != nil {
			return fmt.Errorf("could not count the super admins: %w", err)
		}
		if supers > 0 {
			return ErrFirstAdminClaimed
		}
		if err := tx.Create(user).Error; err != nil {
			return fmt.Errorf("could not create the first admin: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to make the first admin: %w", err)
	}
	return nil
}
