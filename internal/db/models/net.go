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

// Net represents a single net check-in session on a talkgroup.
// A net is a period during which transmissions on a talkgroup are
// tracked as "check-ins". Nets can be started ad-hoc by an NCO or
// automatically by a ScheduledNet.
type Net struct {
	ID              uint           `json:"id" gorm:"primarykey" msg:"id"`
	TalkgroupID     uint           `json:"talkgroup_id" gorm:"not null;index" msg:"talkgroup_id"`
	Talkgroup       Talkgroup      `json:"talkgroup" gorm:"foreignKey:TalkgroupID" msg:"-"`
	StartedByUserID uint           `json:"started_by_user_id" msg:"started_by_user_id"`
	StartedByUser   User           `json:"started_by_user" gorm:"foreignKey:StartedByUserID" msg:"-"`
	ScheduledNetID  *uint          `json:"scheduled_net_id,omitempty" msg:"scheduled_net_id,omitempty"`
	StartTime       time.Time      `json:"start_time" msg:"start_time"`
	EndTime         *time.Time     `json:"end_time,omitempty" msg:"end_time,omitempty"`
	DurationMinutes *uint          `json:"duration_minutes,omitempty" msg:"duration_minutes,omitempty"`
	Description     string         `json:"description" msg:"description"`
	Active          bool           `json:"active" gorm:"index" msg:"active"`
	Showcase        bool           `json:"showcase" gorm:"default:false" msg:"showcase"`
	CreatedAt       time.Time      `json:"-" msg:"-"`
	UpdatedAt       time.Time      `json:"-" msg:"-"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
}

// FindActiveNetForTalkgroup returns the currently active net for a talkgroup, if any.
func FindActiveNetForTalkgroup(db *gorm.DB, talkgroupID uint) (Net, error) {
	var net Net
	err := db.Preload("Talkgroup").Preload("StartedByUser").
		Where("talkgroup_id = ? AND active = ?", talkgroupID, true).
		First(&net).Error
	return net, err
}

// FindNetByID returns a net by its ID.
func FindNetByID(db *gorm.DB, id uint) (Net, error) {
	var net Net
	err := db.Preload("Talkgroup").Preload("StartedByUser").First(&net, id).Error
	return net, err
}

// FindNetsForTalkgroup returns all nets for a talkgroup, ordered by start time descending.
func FindNetsForTalkgroup(db *gorm.DB, talkgroupID uint) ([]Net, error) {
	var nets []Net
	err := db.Preload("Talkgroup").Preload("StartedByUser").
		Where("talkgroup_id = ?", talkgroupID).
		Order("start_time desc").Find(&nets).Error
	return nets, err
}

// CountNetsForTalkgroup returns the total number of nets for a talkgroup.
func CountNetsForTalkgroup(db *gorm.DB, talkgroupID uint) (int, error) {
	var count int64
	err := db.Model(&Net{}).Where("talkgroup_id = ?", talkgroupID).Count(&count).Error
	return int(count), err
}

// ListNets returns all nets, ordered by active first, then showcase, then by talkgroup ID.
func ListNets(db *gorm.DB) ([]Net, error) {
	var nets []Net
	err := db.Preload("Talkgroup").Preload("StartedByUser").
		Order("active DESC, showcase DESC, talkgroup_id ASC, start_time DESC").Find(&nets).Error
	return nets, err
}

// CountNets returns the total number of nets.
func CountNets(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&Net{}).Count(&count).Error
	return int(count), err
}

// ListActiveNets returns all currently active nets.
func ListActiveNets(db *gorm.DB) ([]Net, error) {
	var nets []Net
	err := db.Preload("Talkgroup").Preload("StartedByUser").
		Where("active = ?", true).
		Order("showcase DESC, talkgroup_id ASC, start_time DESC").Find(&nets).Error
	return nets, err
}

// ListShowcaseNets returns active nets marked as showcase.
func ListShowcaseNets(db *gorm.DB) ([]Net, error) {
	var nets []Net
	err := db.Preload("Talkgroup").Preload("StartedByUser").
		Where("active = ? AND showcase = ?", true, true).
		Order("talkgroup_id ASC, start_time DESC").Find(&nets).Error
	return nets, err
}

// UpdateNetShowcase toggles the showcase flag on a net.
func UpdateNetShowcase(db *gorm.DB, id uint, showcase bool) error {
	result := db.Model(&Net{}).Where("id = ?", id).Update("showcase", showcase)
	if result.Error != nil {
		return fmt.Errorf("failed to update net showcase: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CountActiveNets returns the number of currently active nets.
func CountActiveNets(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&Net{}).Where("active = ?", true).Count(&count).Error
	return int(count), err
}

// CreateNet persists a new net to the database.
func CreateNet(db *gorm.DB, net *Net) error {
	if err := db.Create(net).Error; err != nil {
		return fmt.Errorf("failed to create net: %w", err)
	}
	return nil
}

// EndNet marks a net as inactive and records the end time.
func EndNet(db *gorm.DB, id uint) error {
	now := time.Now()
	result := db.Model(&Net{}).Where("id = ? AND active = ?", id, true).
		Updates(map[string]interface{}{
			"active":   false,
			"end_time": now,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to end net: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteNet soft-deletes a net.
func DeleteNet(db *gorm.DB, id uint) error {
	result := db.Delete(&Net{}, id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete net: %w", result.Error)
	}
	return nil
}
