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

//nolint:golint,wrapcheck
package models

import (
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	gorm_seeder "github.com/kachit/gorm-seeder"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Talkgroup struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Admins      []User         `json:"admins" gorm:"many2many:talkgroup_admins;"`
	NCOs        []User         `json:"ncos" gorm:"many2many:talkgroup_ncos;"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"-"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func ListTalkgroups(db *gorm.DB) ([]Talkgroup, error) {
	var talkgroups []Talkgroup
	err := db.Preload("Admins").Preload("NCOs").Order("id asc").Find(&talkgroups).Error
	return talkgroups, err
}

func CountTalkgroups(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&Talkgroup{}).Count(&count).Error
	return int(count), err
}

func TalkgroupIDExists(db *gorm.DB, id uint) (bool, error) {
	var count int64
	err := db.Model(&Talkgroup{}).Where("ID = ?", id).Limit(1).Count(&count).Error
	return count > 0, err
}

func FindTalkgroupByID(db *gorm.DB, id uint) (Talkgroup, error) {
	var talkgroup Talkgroup
	err := db.Preload("Admins").Preload("NCOs").First(&talkgroup, id).Error
	return talkgroup, err
}

func DeleteTalkgroup(db *gorm.DB, id uint) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		// Delete calls where IsToTalkgroup is true and IsToTalkgroupID is id
		tx.Unscoped().Where("is_to_talkgroup = ? AND to_talkgroup_id = ?", true, id).Delete(&Call{})
		// Find repeaters with TS1DynamicTalkgroup or TS2DynamicTalkgroup set to id
		var repeaters []Repeater
		tx.Where("ts1_dynamic_talkgroup_id = ? OR ts2_dynamic_talkgroup_id = ?", id, id).Find(&repeaters)
		// Set TS1DynamicTalkgroup or TS2DynamicTalkgroup to nil
		for _, repeater := range repeaters {
			repeater := repeater
			if repeater.TS1DynamicTalkgroupID != nil && *repeater.TS1DynamicTalkgroupID == id {
				repeater.TS1DynamicTalkgroup = Talkgroup{}
				repeater.TS1DynamicTalkgroupID = nil
			}
			if repeater.TS2DynamicTalkgroupID != nil && *repeater.TS2DynamicTalkgroupID == id {
				repeater.TS2DynamicTalkgroup = Talkgroup{}
				repeater.TS2DynamicTalkgroupID = nil
			}
			err := tx.Save(&repeater).Error
			if err != nil {
				logging.GetLogger(logging.Error).Logf(DeleteTalkgroup, "Error saving repeater: %s", err)
				return err
			}
		}

		tx.Unscoped().Table("repeater_ts1_static_talkgroups").Where("talkgroup_id = ?", id).Delete(&Repeater{})
		tx.Unscoped().Table("repeater_ts2_static_talkgroups").Where("talkgroup_id = ?", id).Delete(&Repeater{})

		tx.Unscoped().Select(clause.Associations, "Admins").Select(clause.Associations, "NCOs").Delete(&Talkgroup{ID: id})

		return nil
	})
	if err != nil {
		logging.GetLogger(logging.Error).Logf(DeleteTalkgroup, "Error deleting talkgroup: %s", err)
		return err
	}
	return nil
}

func FindTalkgroupsByOwnerID(db *gorm.DB, ownerID uint) ([]Talkgroup, error) {
	var talkgroups []Talkgroup
	if err := db.Joins("JOIN talkgroup_admins on talkgroup_admins.talkgroup_id=talkgroups.id").
		Joins("JOIN users on talkgroup_admins.user_id=users.id").Order("id asc").Where("users.id=?", ownerID).
		Group("talkgroups.id").Find(&talkgroups).Error; err != nil {
		logging.GetLogger(logging.Error).Logf(FindTalkgroupsByOwnerID, "Error getting talkgroups owned by user %d: %v", ownerID, err)
		return nil, err
	}
	return talkgroups, nil
}

func CountTalkgroupsByOwnerID(db *gorm.DB, ownerID uint) (int, error) {
	var count int64
	err := db.Model(&Talkgroup{}).Joins("JOIN talkgroup_admins on talkgroup_admins.talkgroup_id=talkgroups.id").
		Joins("JOIN users on talkgroup_admins.user_id=users.id").
		Where("users.id=?", ownerID).Count(&count).Error
	return int(count), err
}

type TalkgroupsSeeder struct {
	gorm_seeder.SeederAbstract
}

const TalkgroupSeederRows = 1

func NewTalkgroupsSeeder(cfg gorm_seeder.SeederConfiguration) TalkgroupsSeeder {
	return TalkgroupsSeeder{gorm_seeder.NewSeederAbstract(cfg)}
}

func (s *TalkgroupsSeeder) Seed(db *gorm.DB) error {
	var talkgroups = []Talkgroup{
		{
			ID:          dmrconst.ParrotUser,
			Name:        "DMRHub Parrot",
			Description: "This talkgroup will not be routed to any repeaters and Parrot will respond with a private call.",
		},
	}
	return db.CreateInBatches(talkgroups, s.Configuration.Rows).Error
}

func (s *TalkgroupsSeeder) Clear(db *gorm.DB) error {
	return nil
}
