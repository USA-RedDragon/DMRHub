// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"time"

	"gorm.io/gorm"
)

type Call struct {
	ID             uint           `json:"id" gorm:"primarykey"`
	CallData       []byte         `json:"-"`
	StreamID       uint           `json:"-"`
	StartTime      time.Time      `json:"start_time"`
	Duration       time.Duration  `json:"duration"`
	Active         bool           `json:"active"`
	User           User           `json:"user" gorm:"foreignKey:UserID"`
	UserID         uint           `json:"-"`
	Repeater       Repeater       `json:"repeater" gorm:"foreignKey:RepeaterID"`
	RepeaterID     uint           `json:"-"`
	TimeSlot       bool           `json:"time_slot"`
	GroupCall      bool           `json:"group_call"`
	IsToTalkgroup  bool           `json:"is_to_talkgroup"`
	ToTalkgroupID  *uint          `json:"-"`
	ToTalkgroup    Talkgroup      `json:"to_talkgroup" gorm:"foreignKey:ToTalkgroupID"`
	IsToUser       bool           `json:"is_to_user"`
	ToUserID       *uint          `json:"-"`
	ToUser         User           `json:"to_user" gorm:"foreignKey:ToUserID"`
	IsToRepeater   bool           `json:"is_to_repeater"`
	ToRepeaterID   *uint          `json:"-"`
	ToRepeater     Repeater       `json:"to_repeater" gorm:"foreignKey:ToRepeaterID"`
	DestinationID  uint           `json:"destination_id"`
	TotalPackets   uint           `json:"-"`
	LostSequences  uint           `json:"-"`
	Loss           float32        `json:"loss"`
	Jitter         float32        `json:"jitter"`
	LastFrameNum   uint           `json:"-"`
	LastSeq        uint           `json:"-"`
	BER            float32        `json:"ber"`
	RSSI           float32        `json:"rssi"`
	TotalBits      uint           `json:"-"`
	TotalErrors    int            `json:"-"`
	LastPacketTime time.Time      `json:"-"`
	HasHeader      bool           `json:"-"`
	HasTerm        bool           `json:"-"`
	CreatedAt      time.Time      `json:"-"`
	UpdatedAt      time.Time      `json:"-"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`
}

func FindCalls(db *gorm.DB) []Call {
	var calls []Call
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("is_to_talkgroup = ?", true).Order("start_time desc").Find(&calls)
	return calls
}

func CountCalls(db *gorm.DB) int {
	var count int64
	db.Model(&Call{}).Where("is_to_talkgroup = ?", true).Count(&count)
	return int(count)
}

func FindRepeaterCalls(db *gorm.DB, repeaterID uint) []Call {
	var calls []Call
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").
		Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, repeaterID, repeaterID).
		Order("start_time desc").Find(&calls)
	return calls
}

func CountRepeaterCalls(db *gorm.DB, repeaterID uint) int {
	var count int64
	db.Model(&Call{}).Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, repeaterID, repeaterID).Count(&count)
	return int(count)
}

func FindUserCalls(db *gorm.DB, userID uint) []Call {
	var calls []Call
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").
		Where("(is_to_user = ? AND to_user_id = ?) OR user_id = ?", true, userID, userID).
		Order("start_time desc").Find(&calls)
	return calls
}

func CountUserCalls(db *gorm.DB, userID uint) int {
	var count int64
	db.Model(&Call{}).Where("(is_to_user = ? AND to_user_id = ?) OR user_id = ?", true, userID, userID).Count(&count)
	return int(count)
}

func FindTalkgroupCalls(db *gorm.DB, talkgroupID uint) []Call {
	var calls []Call
	// Find calls where (IsToTalkgroup is true and ToTalkgroupID is talkgroupID)
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").
		Where("is_to_talkgroup = ? AND to_talkgroup_id = ?", true, talkgroupID).
		Order("start_time desc").Find(&calls)
	return calls
}

func CountTalkgroupCalls(db *gorm.DB, talkgroupID uint) int {
	var count int64
	db.Model(&Call{}).Where("is_to_talkgroup = ? AND to_talkgroup_id = ?", true, talkgroupID).Count(&count)
	return int(count)
}

func FindActiveCall(db *gorm.DB, streamID uint, src uint, dst uint, slot bool, groupCall bool) (Call, error) {
	var call Call
	err := db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("stream_id = ? AND active = ? AND user_id = ? AND destination_id = ? AND time_slot = ? AND group_call = ?", streamID, true, src, dst, slot, groupCall).First(&call).Error
	if err != nil {
		return call, err
	}
	return call, nil
}

func ActiveCallExists(db *gorm.DB, streamID uint, src uint, dst uint, slot bool, groupCall bool) bool {
	var count int64
	db.Model(&Call{}).Where("stream_id = ? AND active = ? AND user_id = ? AND destination_id = ? AND time_slot = ? AND group_call = ?", streamID, true, src, dst, slot, groupCall).Count(&count)
	return count > 0
}
