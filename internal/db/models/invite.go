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
	"fmt"
	"time"

	"gorm.io/gorm"
)

// InviteLifetime is how long an invite works.
const InviteLifetime = 7 * 24 * time.Hour

const inviteCodeBytes = 16

// Invite lets one person register while registration is by invite only.
// Only the SHA-256 hash of its code is kept. It works once, until
// ExpiresAt.
type Invite struct {
	ID        uint      `gorm:"primaryKey"`
	CodeHash  []byte    `gorm:"size:32;not null;uniqueIndex"`
	CreatedBy uint      `gorm:"not null;index"`
	CreatedAt time.Time `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null;index"`
	UsedBy    *uint
	UsedAt    *time.Time
}

func (Invite) TableName() string {
	return "invites"
}

func hashInviteCode(code string) []byte {
	sum := sha256.Sum256([]byte(code))
	return sum[:]
}

// CreateInvite makes an invite of the admin by that works for
// InviteLifetime after now, and returns it and its code.
func CreateInvite(db *gorm.DB, by uint, now time.Time) (Invite, string, error) {
	var b [inviteCodeBytes]byte
	_, _ = rand.Read(b[:])
	code := hex.EncodeToString(b[:])
	inv := Invite{CodeHash: hashInviteCode(code), CreatedBy: by, CreatedAt: now.UTC(), ExpiresAt: now.Add(InviteLifetime).UTC()}
	if err := db.Create(&inv).Error; err != nil {
		return Invite{}, "", fmt.Errorf("could not save an invite: %w", err)
	}
	return inv, code, nil
}

// UseInvite marks the invite of code used by the user id at now. It
// returns false when the invite does not exist, is used or has expired.
func UseInvite(db *gorm.DB, code string, id uint, now time.Time) (bool, error) {
	at := now.UTC()
	res := db.Model(&Invite{}).Where("code_hash = ? AND used_at IS NULL AND expires_at > ?", hashInviteCode(code), at).
		Updates(map[string]any{"used_by": id, "used_at": at})
	if res.Error != nil {
		return false, fmt.Errorf("could not use an invite: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// InviteWorks reports whether the invite of code exists, is unused and has
// not expired at now.
func InviteWorks(db *gorm.DB, code string, now time.Time) (bool, error) {
	var n int64
	if err := db.Model(&Invite{}).Where("code_hash = ? AND used_at IS NULL AND expires_at > ?", hashInviteCode(code), now.UTC()).Count(&n).Error; err != nil {
		return false, fmt.Errorf("could not check an invite: %w", err)
	}
	return n == 1, nil
}

// ListInvites returns the invites, newest first.
func ListInvites(db *gorm.DB) ([]Invite, error) {
	var out []Invite
	if err := db.Order("created_at DESC").Order("id DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("could not list the invites: %w", err)
	}
	return out, nil
}

// DeleteUnusedInvite deletes the invite id when nobody used it, and reports
// whether it did.
func DeleteUnusedInvite(db *gorm.DB, id uint) (bool, error) {
	res := db.Where("id = ? AND used_at IS NULL", id).Delete(&Invite{})
	if res.Error != nil {
		return false, fmt.Errorf("could not delete invite %d: %w", id, res.Error)
	}
	return res.RowsAffected == 1, nil
}
