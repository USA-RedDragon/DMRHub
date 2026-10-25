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

// SetPassword gives the user id the password hash hash. mustChange marks
// a password that an admin set, which the user must replace with their
// own. Every password reset link of the user stops working.
func SetPassword(db *gorm.DB, id uint, hash string, mustChange bool) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE users SET password = ?, must_change_password = ?, updated_at = ? WHERE id = ?", hash, mustChange, time.Now(), id).Error; err != nil {
			return fmt.Errorf("could not update the user: %w", err)
		}
		if err := tx.Where("user_id = ?", id).Delete(&PasswordReset{}).Error; err != nil {
			return fmt.Errorf("could not end the password resets: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("could not set the password of user %d: %w", id, err)
	}
	return nil
}
