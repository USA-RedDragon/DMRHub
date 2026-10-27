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

// RadioIDDrift is a user whose callsign RadioID does not give their DMR
// ID, as the last RadioID drift check found.
type RadioIDDrift struct {
	UserID uint `gorm:"primaryKey;autoIncrement:false"`
	// RadioIDCallsign is the callsign that RadioID gives the DMR ID, or
	// empty when RadioID does not have the DMR ID.
	RadioIDCallsign string
	// FoundAt is when a check first found the user.
	FoundAt time.Time `gorm:"not null"`
	// CheckedAt is when the last check found the user.
	CheckedAt time.Time `gorm:"not null"`
}

func (RadioIDDrift) TableName() string {
	return "radio_id_drifts"
}

// RadioIDDriftRow is a RadioIDDrift with the user that it is about.
type RadioIDDriftRow struct {
	UserID          uint
	Username        string
	Callsign        string
	RadioIDCallsign string
	FoundAt         time.Time
	CheckedAt       time.Time
	Email           string
	EmailVerified   bool
}

// ReplaceRadioIDDrifts makes drifts the only RadioID drifts, found at now.
// A user who was already in the list keeps the time when a check first
// found them.
func ReplaceRadioIDDrifts(db *gorm.DB, drifts []RadioIDDrift, now time.Time) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		var old []RadioIDDrift
		if err := tx.Find(&old).Error; err != nil {
			return fmt.Errorf("could not read the RadioID drifts: %w", err)
		}
		found := make(map[uint]time.Time, len(old))
		for _, d := range old {
			found[d.UserID] = d.FoundAt
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
			if at, ok := found[d.UserID]; ok {
				d.FoundAt = at
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

// DeleteRadioIDDrift removes the user id from the RadioID drifts.
func DeleteRadioIDDrift(db *gorm.DB, id uint) error {
	if err := db.Where("user_id = ?", id).Delete(&RadioIDDrift{}).Error; err != nil {
		return fmt.Errorf("could not remove the RadioID drift of %d: %w", id, err)
	}
	return nil
}

// FindRadioIDDrift returns the RadioID drift of the user id.
func FindRadioIDDrift(db *gorm.DB, id uint) (RadioIDDrift, bool, error) {
	var drifts []RadioIDDrift
	if err := db.Where("user_id = ?", id).Limit(1).Find(&drifts).Error; err != nil {
		return RadioIDDrift{}, false, fmt.Errorf("could not find the RadioID drift of %d: %w", id, err)
	}
	if len(drifts) == 0 {
		return RadioIDDrift{}, false, nil
	}
	return drifts[0], true, nil
}

func radioIDDriftRows(db *gorm.DB) *gorm.DB {
	return db.Table("radio_id_drifts").Joins("JOIN users ON users.id = radio_id_drifts.user_id AND users.deleted_at IS NULL")
}

// ListRadioIDDrifts returns the RadioID drifts of users who are not
// deleted, the oldest found first. db may be paginated.
func ListRadioIDDrifts(db *gorm.DB) ([]RadioIDDriftRow, error) {
	var rows []RadioIDDriftRow
	err := radioIDDriftRows(db).
		Select("radio_id_drifts.user_id, users.username, users.callsign, radio_id_drifts.radio_id_callsign, radio_id_drifts.found_at, radio_id_drifts.checked_at, users.email, users.email_verified").
		Order("radio_id_drifts.found_at").Order("radio_id_drifts.user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the RadioID drifts: %w", err)
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
