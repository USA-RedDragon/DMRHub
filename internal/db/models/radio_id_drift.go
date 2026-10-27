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

// RadioIDDrift is a radio ID of a user, their account ID or one they
// added, that RadioID does not list under the callsign of the user, as the
// last RadioID drift check found.
type RadioIDDrift struct {
	RadioID uint `gorm:"primaryKey;autoIncrement:false"`
	UserID  uint `gorm:"not null;index"`
	// RadioIDCallsign is the callsign that RadioID lists for the radio
	// ID, or empty when RadioID does not have the radio ID.
	RadioIDCallsign string
	// FoundAt is when a check first found the radio ID.
	FoundAt time.Time `gorm:"not null"`
	// CheckedAt is when the last check found the radio ID.
	CheckedAt time.Time `gorm:"not null"`
	// NotifiedAt is when the user got an email about the radio ID, or
	// nil when they did not.
	NotifiedAt *time.Time
}

func (RadioIDDrift) TableName() string {
	return "radio_id_drifts"
}

// RadioIDDriftRow is a RadioIDDrift with the user that it is about.
type RadioIDDriftRow struct {
	RadioID         uint
	UserID          uint
	Username        string
	Callsign        string
	RadioIDCallsign string
	FoundAt         time.Time
	CheckedAt       time.Time
	NotifiedAt      *time.Time
	Email           string
	EmailVerified   bool
}

// ReplaceRadioIDDrifts makes drifts the only RadioID drifts, found at now.
// A radio ID that was already in the list keeps the time when a check
// first found it and the time of its email.
func ReplaceRadioIDDrifts(db *gorm.DB, drifts []RadioIDDrift, now time.Time) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		var old []RadioIDDrift
		if err := tx.Find(&old).Error; err != nil {
			return fmt.Errorf("could not read the RadioID drifts: %w", err)
		}
		before := make(map[uint]RadioIDDrift, len(old))
		for _, d := range old {
			before[d.RadioID] = d
		}
		if err := tx.Where("1 = 1").Delete(&RadioIDDrift{}).Error; err != nil {
			return fmt.Errorf("could not clear the RadioID drifts: %w", err)
		}
		if len(drifts) == 0 {
			return nil
		}
		rows := make([]RadioIDDrift, len(drifts))
		for i, d := range drifts {
			d.CheckedAt = now
			d.FoundAt = now
			d.NotifiedAt = nil
			if b, ok := before[d.RadioID]; ok && b.UserID == d.UserID {
				d.FoundAt = b.FoundAt
				d.NotifiedAt = b.NotifiedAt
			}
			rows[i] = d
		}
		if err := tx.CreateInBatches(rows, 500).Error; err != nil {
			return fmt.Errorf("could not store the RadioID drifts: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("could not replace the RadioID drifts: %w", err)
	}
	return nil
}

// DeleteRadioIDDrift removes the radio ID id from the RadioID drifts.
func DeleteRadioIDDrift(db *gorm.DB, id uint) error {
	if err := db.Where("radio_id = ?", id).Delete(&RadioIDDrift{}).Error; err != nil {
		return fmt.Errorf("could not remove the RadioID drift of %d: %w", id, err)
	}
	return nil
}

// DeleteUserRadioIDDrifts removes every radio ID of the user userID from
// the RadioID drifts.
func DeleteUserRadioIDDrifts(db *gorm.DB, userID uint) error {
	if err := db.Where("user_id = ?", userID).Delete(&RadioIDDrift{}).Error; err != nil {
		return fmt.Errorf("could not remove the RadioID drifts of user %d: %w", userID, err)
	}
	return nil
}

// ResolveRadioIDDrifts removes the radio IDs of the user userID that
// RadioID lists under callsign from the RadioID drifts.
func ResolveRadioIDDrifts(db *gorm.DB, userID uint, callsign string) error {
	if err := db.Where("user_id = ? AND UPPER(radio_id_callsign) = UPPER(?)", userID, callsign).Delete(&RadioIDDrift{}).Error; err != nil {
		return fmt.Errorf("could not remove the RadioID drifts of user %d: %w", userID, err)
	}
	return nil
}

// FindRadioIDDrift returns the RadioID drift of the radio ID id.
func FindRadioIDDrift(db *gorm.DB, id uint) (RadioIDDrift, bool, error) {
	var drifts []RadioIDDrift
	if err := db.Where("radio_id = ?", id).Limit(1).Find(&drifts).Error; err != nil {
		return RadioIDDrift{}, false, fmt.Errorf("could not find the RadioID drift of %d: %w", id, err)
	}
	if len(drifts) == 0 {
		return RadioIDDrift{}, false, nil
	}
	return drifts[0], true, nil
}

// MarkRadioIDDriftNotified records that the user got an email about the
// radio ID id at at.
func MarkRadioIDDriftNotified(db *gorm.DB, id uint, at time.Time) error {
	if err := db.Model(&RadioIDDrift{}).Where("radio_id = ?", id).Update("notified_at", at).Error; err != nil {
		return fmt.Errorf("could not mark the RadioID drift of %d notified: %w", id, err)
	}
	return nil
}

func radioIDDriftRows(db *gorm.DB) *gorm.DB {
	return db.Table("radio_id_drifts").Joins("JOIN users ON users.id = radio_id_drifts.user_id AND users.deleted_at IS NULL")
}

const radioIDDriftColumns = "radio_id_drifts.radio_id, radio_id_drifts.user_id, users.username, users.callsign, radio_id_drifts.radio_id_callsign, " +
	"radio_id_drifts.found_at, radio_id_drifts.checked_at, radio_id_drifts.notified_at, users.email, users.email_verified"

// ListRadioIDDrifts returns the RadioID drifts of users who are not
// deleted, the oldest found first. db may be paginated.
func ListRadioIDDrifts(db *gorm.DB) ([]RadioIDDriftRow, error) {
	var rows []RadioIDDriftRow
	err := radioIDDriftRows(db).Select(radioIDDriftColumns).
		Order("radio_id_drifts.found_at").Order("radio_id_drifts.radio_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the RadioID drifts: %w", err)
	}
	return rows, nil
}

// UnnotifiedRadioIDDrifts returns the RadioID drifts of users who are not
// deleted and did not get an email about them, whose user verified an
// email address.
func UnnotifiedRadioIDDrifts(db *gorm.DB) ([]RadioIDDriftRow, error) {
	var rows []RadioIDDriftRow
	err := radioIDDriftRows(db).Select(radioIDDriftColumns).
		Where("radio_id_drifts.notified_at IS NULL AND users.email_verified = ? AND users.email <> ''", true).
		Order("radio_id_drifts.radio_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the RadioID drifts to email: %w", err)
	}
	return rows, nil
}

// CountRadioIDDrifts counts the RadioID drifts of users who are not
// deleted.
func CountRadioIDDrifts(db *gorm.DB) (int64, error) {
	var n int64
	if err := radioIDDriftRows(db).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("could not count the RadioID drifts: %w", err)
	}
	return n, nil
}
