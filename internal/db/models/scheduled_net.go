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

//go:generate go run github.com/tinylib/msgp

// ScheduledNet represents a recurring net schedule for a talkgroup.
// DayOfWeek and TimeOfDay are stored in UTC (the frontend converts from
// the user's local time before sending). Timezone is stored for display
// purposes so the UI can show the schedule in the creator's local time.
// The CronExpression is generated from the UTC DayOfWeek and TimeOfDay.
type ScheduledNet struct {
	ID              uint           `json:"id" gorm:"primarykey" msg:"id"`
	TalkgroupID     uint           `json:"talkgroup_id" gorm:"not null;index" msg:"talkgroup_id"`
	Talkgroup       Talkgroup      `json:"talkgroup" gorm:"foreignKey:TalkgroupID" msg:"-"`
	CreatedByUserID uint           `json:"created_by_user_id" msg:"created_by_user_id"`
	CreatedByUser   User           `json:"created_by_user" gorm:"foreignKey:CreatedByUserID" msg:"-"`
	Name            string         `json:"name" msg:"name"`
	Description     string         `json:"description" msg:"description"`
	CronExpression  string         `json:"cron_expression" msg:"cron_expression"`
	DayOfWeek       int            `json:"day_of_week" msg:"day_of_week"`
	TimeOfDay       string         `json:"time_of_day" msg:"time_of_day"`
	Timezone        string         `json:"timezone" msg:"timezone"`
	DurationMinutes *uint          `json:"duration_minutes,omitempty" msg:"duration_minutes,omitempty"`
	Enabled         bool           `json:"enabled" gorm:"default:true" msg:"enabled"`
	Showcase        bool           `json:"showcase" gorm:"default:false" msg:"showcase"`
	NextRun         *time.Time     `json:"next_run,omitempty" msg:"next_run,omitempty"`
	CreatedAt       time.Time      `json:"created_at" msg:"-"`
	UpdatedAt       time.Time      `json:"-" msg:"-"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
}

// GenerateCronExpression builds a cron expression from schedule fields.
// The cron format is: minute hour * * day_of_week (evaluated in UTC).
// DayOfWeek uses 0=Sunday..6=Saturday. TimeOfDay is "HH:MM" 24h UTC.
func GenerateCronExpression(dayOfWeek int, timeOfDay string) (string, error) {
	if dayOfWeek < 0 || dayOfWeek > 6 {
		return "", fmt.Errorf("invalid day of week: %d (must be 0-6)", dayOfWeek)
	}

	var hour, minute int
	n, err := fmt.Sscanf(timeOfDay, "%d:%d", &hour, &minute)
	if err != nil || n != 2 {
		return "", fmt.Errorf("invalid time of day: %q (must be HH:MM)", timeOfDay)
	}
	if hour < 0 || hour > 23 {
		return "", fmt.Errorf("invalid hour: %d (must be 0-23)", hour)
	}
	if minute < 0 || minute > 59 {
		return "", fmt.Errorf("invalid minute: %d (must be 0-59)", minute)
	}

	return fmt.Sprintf("%d %d * * %d", minute, hour, dayOfWeek), nil
}

// FindScheduledNetByID returns a scheduled net by its ID.
func FindScheduledNetByID(db *gorm.DB, id uint) (ScheduledNet, error) {
	var sn ScheduledNet
	err := db.Preload("Talkgroup").Preload("CreatedByUser").First(&sn, id).Error
	return sn, err
}

// FindScheduledNetsForTalkgroup returns all scheduled nets for a talkgroup.
func FindScheduledNetsForTalkgroup(db *gorm.DB, talkgroupID uint) ([]ScheduledNet, error) {
	var nets []ScheduledNet
	err := db.Preload("Talkgroup").Preload("CreatedByUser").
		Where("talkgroup_id = ?", talkgroupID).
		Order("created_at desc").Find(&nets).Error
	return nets, err
}

// CountScheduledNetsForTalkgroup returns the count of scheduled nets for a talkgroup.
func CountScheduledNetsForTalkgroup(db *gorm.DB, talkgroupID uint) (int, error) {
	var count int64
	err := db.Model(&ScheduledNet{}).Where("talkgroup_id = ?", talkgroupID).Count(&count).Error
	return int(count), err
}

// FindAllEnabledScheduledNets returns all enabled scheduled nets across all talkgroups.
func FindAllEnabledScheduledNets(db *gorm.DB) ([]ScheduledNet, error) {
	var nets []ScheduledNet
	err := db.Preload("Talkgroup").Preload("CreatedByUser").
		Where("enabled = ?", true).Find(&nets).Error
	return nets, err
}

// ListScheduledNets returns all scheduled nets, ordered by creation time descending.
func ListScheduledNets(db *gorm.DB) ([]ScheduledNet, error) {
	var nets []ScheduledNet
	err := db.Preload("Talkgroup").Preload("CreatedByUser").
		Order("created_at desc").Find(&nets).Error
	return nets, err
}

// CountScheduledNets returns the total number of scheduled nets.
func CountScheduledNets(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&ScheduledNet{}).Count(&count).Error
	return int(count), err
}

// CreateScheduledNet persists a new scheduled net to the database.
func CreateScheduledNet(db *gorm.DB, sn *ScheduledNet) error {
	if err := db.Create(sn).Error; err != nil {
		return fmt.Errorf("failed to create scheduled net: %w", err)
	}
	return nil
}

// UpdateScheduledNet updates a scheduled net in the database.
func UpdateScheduledNet(db *gorm.DB, sn *ScheduledNet) error {
	if err := db.Save(sn).Error; err != nil {
		return fmt.Errorf("failed to update scheduled net: %w", err)
	}
	return nil
}

// DeleteScheduledNet soft-deletes a scheduled net.
func DeleteScheduledNet(db *gorm.DB, id uint) error {
	result := db.Delete(&ScheduledNet{}, id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete scheduled net: %w", result.Error)
	}
	return nil
}
