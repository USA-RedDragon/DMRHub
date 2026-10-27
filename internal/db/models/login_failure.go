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

const (
	// LoginFailureLimit is how many failed logins of one account
	// LoginFailureWindow allows. The next login of the account is refused
	// until the oldest of them is older than the window.
	LoginFailureLimit = 10
	// LoginFailureWindow is how long a failed login counts.
	LoginFailureWindow = 15 * time.Minute
)

// LoginFailure is one failed login of an account: of a user, by its ID,
// or of a name that no user has, so that both answer alike.
type LoginFailure struct {
	ID      uint      `gorm:"primaryKey"`
	Account string    `gorm:"size:80;not null;index:idx_login_failures_account_at,priority:1"`
	At      time.Time `gorm:"not null;index;index:idx_login_failures_account_at,priority:2"`
}

func (LoginFailure) TableName() string {
	return "login_failures"
}

// LoginBlocked reports whether account had LoginFailureLimit failed logins
// in the LoginFailureWindow before now.
func LoginBlocked(db *gorm.DB, account string, now time.Time) (bool, error) {
	var n int64
	if err := db.Model(&LoginFailure{}).Where("account = ? AND at > ?", account, now.Add(-LoginFailureWindow).UTC()).Count(&n).Error; err != nil {
		return false, fmt.Errorf("could not count the failed logins: %w", err)
	}
	return n >= LoginFailureLimit, nil
}

// AddLoginFailure stores a failed login of account at now, and deletes the
// failed logins of every account that no longer count.
func AddLoginFailure(db *gorm.DB, account string, now time.Time) error {
	if err := db.Create(&LoginFailure{Account: account, At: now.UTC()}).Error; err != nil {
		return fmt.Errorf("could not store a failed login: %w", err)
	}
	if err := db.Where("at <= ?", now.Add(-LoginFailureWindow).UTC()).Delete(&LoginFailure{}).Error; err != nil {
		return fmt.Errorf("could not delete the old failed logins: %w", err)
	}
	return nil
}

// ClearLoginFailures deletes the failed logins of account.
func ClearLoginFailures(db *gorm.DB, account string) error {
	if err := db.Where("account = ?", account).Delete(&LoginFailure{}).Error; err != nil {
		return fmt.Errorf("could not delete the failed logins: %w", err)
	}
	return nil
}
