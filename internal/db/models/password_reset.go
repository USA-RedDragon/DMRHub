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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// PasswordResetLifetime is how long the link of a forgotten password
// works.
const PasswordResetLifetime = time.Hour

const passwordResetTokenBytes = 32

// PasswordReset is a link that lets a user who forgot their password
// choose a new one. Only the SHA-256 hash of its token is kept. It works
// once, until ExpiresAt, and any new password of the user ends it.
type PasswordReset struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"not null;index"`
	TokenHash []byte    `gorm:"size:32;not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

func (PasswordReset) TableName() string {
	return "password_resets"
}

func hashPasswordResetToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreatePasswordReset makes a link for the user id that works for
// PasswordResetLifetime after now, and returns its token.
func CreatePasswordReset(db *gorm.DB, id uint, now time.Time) (string, error) {
	var b [passwordResetTokenBytes]byte
	_, _ = rand.Read(b[:])
	token := hex.EncodeToString(b[:])
	reset := PasswordReset{UserID: id, TokenHash: hashPasswordResetToken(token), ExpiresAt: now.Add(PasswordResetLifetime).UTC(), CreatedAt: now.UTC()}
	if err := db.Create(&reset).Error; err != nil {
		return "", fmt.Errorf("could not save a password reset of user %d: %w", id, err)
	}
	return token, nil
}

// PasswordResetUser returns the user of the link token, and false when
// the link does not work at now.
func PasswordResetUser(db *gorm.DB, token string, now time.Time) (User, bool, error) {
	var reset PasswordReset
	err := db.Where("token_hash = ? AND expires_at > ?", hashPasswordResetToken(token), now.UTC()).First(&reset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("could not find a password reset: %w", err)
	}
	var user User
	err = db.First(&user, reset.UserID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("could not find the user of a password reset: %w", err)
	}
	return user, true, nil
}

// ResetPassword gives the user of the link token the password hash hash,
// when the link works at now. It returns the user's ID, and false when the
// link does not work. The link works only once.
func ResetPassword(db *gorm.DB, token, hash string, now time.Time) (uint, bool, error) {
	var id uint
	err := db.Transaction(func(tx *gorm.DB) error {
		var reset PasswordReset
		err := tx.Where("token_hash = ? AND expires_at > ?", hashPasswordResetToken(token), now.UTC()).First(&reset).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("could not find a password reset: %w", err)
		}
		used := tx.Where("id = ?", reset.ID).Delete(&PasswordReset{})
		if used.Error != nil {
			return fmt.Errorf("could not use a password reset: %w", used.Error)
		}
		if used.RowsAffected != 1 {
			return nil
		}
		if err := SetPassword(tx, reset.UserID, hash, false); err != nil {
			return err
		}
		id = reset.UserID
		return nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("could not reset a password: %w", err)
	}
	return id, id != 0, nil
}

// DeleteExpiredPasswordResets deletes the links that stopped working
// before now.
func DeleteExpiredPasswordResets(db *gorm.DB, now time.Time) error {
	if err := db.Where("expires_at <= ?", now.UTC()).Delete(&PasswordReset{}).Error; err != nil {
		return fmt.Errorf("could not delete expired password resets: %w", err)
	}
	return nil
}
