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
	"gorm.io/gorm/clause"
)

// OfflineAlertDelays returns the minutes that a user may pick as the
// delay of an offline alert.
func OfflineAlertDelays() []uint {
	return []uint{5, 10, 30, 60}
}

// OfflineAlert is the choice of a user to get an email when a repeater is
// offline for DelayMinutes, and with Back also when it is back. SentAt is
// when the offline email of the outage that began at Since went out; it
// is nil while no outage was reported.
type OfflineAlert struct {
	UserID       uint       `gorm:"primaryKey;autoIncrement:false"`
	RepeaterID   uint       `gorm:"primaryKey;autoIncrement:false;index"`
	DelayMinutes uint       `gorm:"not null;default:10"`
	Back         bool       `gorm:"not null;default:false"`
	CreatedAt    time.Time  `gorm:"not null"`
	SentAt       *time.Time `gorm:""`
	Since        *time.Time `gorm:""`
}

func (OfflineAlert) TableName() string {
	return "offline_alerts"
}

// ValidOfflineAlertDelay reports whether minutes is one of
// OfflineAlertDelays.
func ValidOfflineAlertDelay(minutes uint) bool {
	for _, d := range OfflineAlertDelays() {
		if d == minutes {
			return true
		}
	}
	return false
}

// FindOfflineAlert returns the offline alert of the user for the repeater.
func FindOfflineAlert(db *gorm.DB, userID, repeaterID uint) (OfflineAlert, bool, error) {
	var out []OfflineAlert
	if err := db.Where("user_id = ? AND repeater_id = ?", userID, repeaterID).Limit(1).Find(&out).Error; err != nil {
		return OfflineAlert{}, false, fmt.Errorf("could not read the offline alert of repeater %d: %w", repeaterID, err)
	}
	if len(out) == 0 {
		return OfflineAlert{}, false, nil
	}
	return out[0], true, nil
}

// SetOfflineAlert turns the offline alert of the user for the repeater on
// with delay and back, made at now when it is new, or changes its delay
// and back.
func SetOfflineAlert(db *gorm.DB, userID, repeaterID, delay uint, back bool, now time.Time) error {
	a := OfflineAlert{UserID: userID, RepeaterID: repeaterID, DelayMinutes: delay, Back: back, CreatedAt: now.UTC()}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "repeater_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"delay_minutes", "back"}),
	}).Create(&a).Error
	if err != nil {
		return fmt.Errorf("could not save the offline alert of repeater %d: %w", repeaterID, err)
	}
	return nil
}

// DeleteOfflineAlert turns the offline alert of the user for the
// repeater off.
func DeleteOfflineAlert(db *gorm.DB, userID, repeaterID uint) error {
	if err := db.Where("user_id = ? AND repeater_id = ?", userID, repeaterID).Delete(&OfflineAlert{}).Error; err != nil {
		return fmt.Errorf("could not delete the offline alert of repeater %d: %w", repeaterID, err)
	}
	return nil
}

// ClaimOfflineAlert marks the offline email of the outage of a that began
// at since as sent at now, and reports false when another replica marked
// it first or the alert is gone.
func ClaimOfflineAlert(db *gorm.DB, a *OfflineAlert, since, now time.Time) (bool, error) {
	res := db.Model(&OfflineAlert{}).Where("user_id = ? AND repeater_id = ? AND sent_at IS NULL", a.UserID, a.RepeaterID).
		Updates(map[string]any{"sent_at": now.UTC(), "since": since.UTC()})
	if res.Error != nil {
		return false, fmt.Errorf("could not claim the offline alert of repeater %d: %w", a.RepeaterID, res.Error)
	}
	return res.RowsAffected == 1, nil
}

// ReleaseOfflineAlert clears the reported outage of a, after the repeater
// came back or the email failed, and reports false when another replica
// cleared it first.
func ReleaseOfflineAlert(db *gorm.DB, a *OfflineAlert) (bool, error) {
	res := db.Model(&OfflineAlert{}).Where("user_id = ? AND repeater_id = ? AND sent_at IS NOT NULL", a.UserID, a.RepeaterID).
		Updates(map[string]any{"sent_at": gorm.Expr("NULL"), "since": gorm.Expr("NULL")})
	if res.Error != nil {
		return false, fmt.Errorf("could not clear the offline alert of repeater %d: %w", a.RepeaterID, res.Error)
	}
	return res.RowsAffected == 1, nil
}
