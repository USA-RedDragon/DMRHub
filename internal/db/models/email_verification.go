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
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// EmailVerificationLifetime is how long the link that verifies an email
// address works.
const EmailVerificationLifetime = 24 * time.Hour

// EmailVerification is a link that verifies the email address Email of a
// user. Only the SHA-256 of its token is kept, and it works once.
type EmailVerification struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"not null;index"`
	Email     string    `gorm:"not null"`
	TokenHash []byte    `gorm:"size:32;not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

// TableName returns the table of the email verification links.
func (EmailVerification) TableName() string {
	return "email_verifications"
}

// SetEmail sets the email address of the user id to email, which is not
// verified until its link is used, and ends the links of an older address.
// An empty email removes the address.
func SetEmail(db *gorm.DB, id uint, email string) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE users SET email = ?, email_verified = ?, updated_at = ? WHERE id = ?", email, false, time.Now(), id).Error; err != nil {
			return fmt.Errorf("could not update the user: %w", err)
		}
		if err := tx.Where("user_id = ?", id).Delete(&EmailVerification{}).Error; err != nil {
			return fmt.Errorf("could not end the email verifications: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("could not set the email of user %d: %w", id, err)
	}
	return nil
}

// CreateEmailVerification saves a link that verifies the address email of
// the user id, and returns its token.
func CreateEmailVerification(db *gorm.DB, id uint, email string, now time.Time) (string, error) {
	var b [passwordResetTokenBytes]byte
	_, _ = rand.Read(b[:])
	token := hex.EncodeToString(b[:])
	v := EmailVerification{UserID: id, Email: email, TokenHash: hashPasswordResetToken(token), ExpiresAt: now.Add(EmailVerificationLifetime).UTC(), CreatedAt: now.UTC()}
	if err := db.Create(&v).Error; err != nil {
		return "", fmt.Errorf("could not save an email verification of user %d: %w", id, err)
	}
	return token, nil
}

// VerifyEmail uses the link of token, which verifies the email address of
// its user when the address is still the one the link was sent to. It
// reports whether it verified one.
func VerifyEmail(db *gorm.DB, token string, now time.Time) (bool, error) {
	verified := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var links []EmailVerification
		if err := tx.Where("token_hash = ? AND expires_at > ?", hashPasswordResetToken(token), now.UTC()).Limit(1).Find(&links).Error; err != nil {
			return fmt.Errorf("could not find an email verification: %w", err)
		}
		if len(links) == 0 {
			return nil
		}
		used := tx.Where("id = ?", links[0].ID).Delete(&EmailVerification{})
		if used.Error != nil {
			return fmt.Errorf("could not use an email verification: %w", used.Error)
		}
		if used.RowsAffected != 1 {
			return nil
		}
		set := tx.Exec("UPDATE users SET email_verified = ? WHERE id = ? AND email = ?", true, links[0].UserID, links[0].Email)
		if set.Error != nil {
			return fmt.Errorf("could not verify the email of user %d: %w", links[0].UserID, set.Error)
		}
		verified = set.RowsAffected == 1
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("could not verify an email: %w", err)
	}
	return verified, nil
}

// DeleteExpiredEmailVerifications deletes the links that no longer work.
func DeleteExpiredEmailVerifications(db *gorm.DB, now time.Time) error {
	if err := db.Where("expires_at <= ?", now.UTC()).Delete(&EmailVerification{}).Error; err != nil {
		return fmt.Errorf("could not delete expired email verifications: %w", err)
	}
	return nil
}
