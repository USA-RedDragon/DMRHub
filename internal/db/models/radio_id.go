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

	"gorm.io/gorm"
)

// ListUserRadioIDs returns the radio IDs that the user userID added, in the
// order they were added. The account ID is not among them.
func ListUserRadioIDs(db *gorm.DB, userID uint) ([]UserRadioID, error) {
	var ids []UserRadioID
	if err := db.Where("user_id = ?", userID).Order("id").Find(&ids).Error; err != nil {
		return nil, fmt.Errorf("could not list radio IDs: %w", err)
	}
	return ids, nil
}

// RadioIDOwner returns the user who owns the subscriber radio ID radioID,
// as an account ID or an added ID. ok is false when no user owns it.
func RadioIDOwner(db *gorm.DB, radioID uint) (userID uint, ok bool, err error) {
	exists, err := UserIDExists(db, radioID)
	if err != nil {
		return 0, false, fmt.Errorf("could not find the user of a radio ID: %w", err)
	}
	if exists {
		return radioID, true, nil
	}
	var ids []UserRadioID
	if err := db.Where("radio_id = ?", radioID).Limit(1).Find(&ids).Error; err != nil {
		return 0, false, fmt.Errorf("could not find the user of a radio ID: %w", err)
	}
	if len(ids) == 0 {
		return 0, false, nil
	}
	return ids[0].UserID, true, nil
}

// ReloadSMSStorage reads the SMS Storage setting of the user again.
func ReloadSMSStorage(db *gorm.DB, user *User) error {
	var row struct {
		SMSStorageEnabled bool
		SMSStorageSince   *time.Time
	}
	if err := db.Model(&User{}).Select("sms_storage_enabled", "sms_storage_since").Where("id = ?", user.ID).Take(&row).Error; err != nil {
		return fmt.Errorf("could not read SMS Storage: %w", err)
	}
	user.SMSStorageEnabled, user.SMSStorageSince = row.SMSStorageEnabled, row.SMSStorageSince
	return nil
}

// SetSMSStorage turns SMS Storage of the user on or off. Turning it on
// when it is off sets the start time to now; turning it on when it is on
// changes nothing.
func SetSMSStorage(db *gorm.DB, user *User, enabled bool, now time.Time) error {
	switch {
	case enabled && user.SMSStorageEnabled:
		return nil
	case enabled:
		at := now.UTC()
		if err := db.Exec("UPDATE users SET sms_storage_enabled = ?, sms_storage_since = ? WHERE id = ?", true, at, user.ID).Error; err != nil {
			return fmt.Errorf("could not turn SMS Storage on: %w", err)
		}
		user.SMSStorageEnabled, user.SMSStorageSince = true, &at
	default:
		if err := db.Exec("UPDATE users SET sms_storage_enabled = ?, sms_storage_since = NULL WHERE id = ?", false, user.ID).Error; err != nil {
			return fmt.Errorf("could not turn SMS Storage off: %w", err)
		}
		user.SMSStorageEnabled, user.SMSStorageSince = false, nil
	}
	return nil
}
