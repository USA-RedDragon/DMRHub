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

// CosysopInviteLifetime is how long an invite to be a co-sysop waits for
// its answer.
const CosysopInviteLifetime = 7 * 24 * time.Hour

var (
	// ErrCosysopIsOwner is an invite of the owner of the repeater.
	ErrCosysopIsOwner = errors.New("the user owns the repeater")
	// ErrCosysopExists is an invite of a user who is a co-sysop of the
	// repeater or has an invite that waits.
	ErrCosysopExists = errors.New("the user is a co-sysop or has an invite")
)

// RepeaterCosysop is a user that the owner of a repeater invited to manage
// it too. The user is a co-sysop from AcceptedAt on. An invite with no
// AcceptedAt waits until ExpiresAt.
type RepeaterCosysop struct {
	RepeaterID uint       `gorm:"primaryKey;autoIncrement:false"`
	UserID     uint       `gorm:"primaryKey;autoIncrement:false;index"`
	InvitedBy  uint       `gorm:"not null"`
	InvitedAt  time.Time  `gorm:"not null"`
	ExpiresAt  time.Time  `gorm:"not null"`
	AcceptedAt *time.Time `gorm:""`
}

func (RepeaterCosysop) TableName() string {
	return "repeater_cosysops"
}

// CosysopEntry is a co-sysop or an open invite of a repeater, with its
// user.
type CosysopEntry struct {
	RepeaterCosysop
	Callsign string
	Username string
}

// IsRepeaterCosysop reports whether the user accepted to be a co-sysop of
// the repeater.
func IsRepeaterCosysop(db *gorm.DB, repeaterID, userID uint) (bool, error) {
	var n int64
	err := db.Model(&RepeaterCosysop{}).Where("repeater_id = ? AND user_id = ? AND accepted_at IS NOT NULL", repeaterID, userID).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("could not check the co-sysops of repeater %d: %w", repeaterID, err)
	}
	return n > 0, nil
}

// CosysopRepeaterIDs lists the repeaters that the user accepted to be a
// co-sysop of.
func CosysopRepeaterIDs(db *gorm.DB, userID uint) ([]uint, error) {
	var ids []uint
	err := db.Model(&RepeaterCosysop{}).Where("user_id = ? AND accepted_at IS NOT NULL", userID).Pluck("repeater_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the co-sysop repeaters of user %d: %w", userID, err)
	}
	return ids, nil
}

// InviteCosysop invites the user to be a co-sysop of the repeater, for
// CosysopInviteLifetime after now. An expired invite of the user is
// replaced.
func InviteCosysop(db *gorm.DB, repeaterID, userID, by uint, now time.Time) (RepeaterCosysop, error) {
	inv := RepeaterCosysop{RepeaterID: repeaterID, UserID: userID, InvitedBy: by, InvitedAt: now.UTC(), ExpiresAt: now.Add(CosysopInviteLifetime).UTC()}
	err := db.Transaction(func(tx *gorm.DB) error {
		var rpt Repeater
		if err := tx.Select("id", "owner_id").First(&rpt, repeaterID).Error; err != nil {
			return fmt.Errorf("could not find repeater %d: %w", repeaterID, err)
		}
		if rpt.OwnerID == userID {
			return ErrCosysopIsOwner
		}
		res := tx.Where("repeater_id = ? AND user_id = ? AND accepted_at IS NULL AND expires_at <= ?", repeaterID, userID, now.UTC()).Delete(&RepeaterCosysop{})
		if res.Error != nil {
			return fmt.Errorf("could not delete an expired invite: %w", res.Error)
		}
		res = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&inv)
		if res.Error != nil {
			return fmt.Errorf("could not save an invite: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrCosysopExists
		}
		return nil
	})
	if err != nil {
		return RepeaterCosysop{}, err //nolint:wrapcheck
	}
	return inv, nil
}

// AnswerCosysopInvite accepts, or with accept false declines, the invite of
// the user to the repeater, and reports false when no invite waits at now.
func AnswerCosysopInvite(db *gorm.DB, repeaterID, userID uint, accept bool, now time.Time) (bool, error) {
	q := db.Model(&RepeaterCosysop{}).Where("repeater_id = ? AND user_id = ? AND accepted_at IS NULL AND expires_at > ?", repeaterID, userID, now.UTC())
	var res *gorm.DB
	if accept {
		res = q.Update("accepted_at", now.UTC())
	} else {
		res = q.Delete(&RepeaterCosysop{})
	}
	if res.Error != nil {
		return false, fmt.Errorf("could not answer the invite to repeater %d: %w", repeaterID, res.Error)
	}
	return res.RowsAffected == 1, nil
}

// RemoveCosysop removes the user from the co-sysops of the repeater, or
// takes back an invite, and reports whether there was one. The offline
// alert of the user for the repeater ends with the role.
func RemoveCosysop(db *gorm.DB, repeaterID, userID uint) (bool, error) {
	removed := false
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("repeater_id = ? AND user_id = ?", repeaterID, userID).Delete(&RepeaterCosysop{})
		if res.Error != nil {
			return res.Error
		}
		removed = res.RowsAffected > 0
		return tx.Where("repeater_id = ? AND user_id = ?", repeaterID, userID).Delete(&OfflineAlert{}).Error
	})
	if err != nil {
		return false, fmt.Errorf("could not remove a co-sysop of repeater %d: %w", repeaterID, err)
	}
	return removed, nil
}

// ListCosysops returns the co-sysops of the repeater, oldest first, and
// with pending true also the invites that wait at now.
func ListCosysops(db *gorm.DB, repeaterID uint, pending bool, now time.Time) ([]CosysopEntry, error) {
	q := db.Table("repeater_cosysops").
		Select("repeater_cosysops.*, users.callsign, users.username").
		Joins("JOIN users ON users.id = repeater_cosysops.user_id AND users.deleted_at IS NULL").
		Where("repeater_cosysops.repeater_id = ?", repeaterID)
	if pending {
		q = q.Where("repeater_cosysops.accepted_at IS NOT NULL OR repeater_cosysops.expires_at > ?", now.UTC())
	} else {
		q = q.Where("repeater_cosysops.accepted_at IS NOT NULL")
	}
	var out []CosysopEntry
	if err := q.Order("repeater_cosysops.invited_at ASC").Order("repeater_cosysops.user_id ASC").Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("could not list the co-sysops of repeater %d: %w", repeaterID, err)
	}
	return out, nil
}

// PendingCosysopInvites returns the invites of the user that wait at now,
// oldest first.
func PendingCosysopInvites(db *gorm.DB, userID uint, now time.Time) ([]RepeaterCosysop, error) {
	var out []RepeaterCosysop
	err := db.Where("user_id = ? AND accepted_at IS NULL AND expires_at > ?", userID, now.UTC()).
		Order("invited_at ASC").Order("repeater_id ASC").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the invites of user %d: %w", userID, err)
	}
	return out, nil
}

const cosysopOf = "id IN (SELECT repeater_id FROM repeater_cosysops WHERE user_id = ? AND accepted_at IS NOT NULL)"

// PurgeExpiredCosysopInvites deletes the invites that expired before now,
// and returns how many.
func PurgeExpiredCosysopInvites(db *gorm.DB, now time.Time) (int, error) {
	res := db.Where("accepted_at IS NULL AND expires_at <= ?", now.UTC()).Delete(&RepeaterCosysop{})
	if res.Error != nil {
		return 0, fmt.Errorf("could not purge the expired co-sysop invites: %w", res.Error)
	}
	return int(res.RowsAffected), nil
}
