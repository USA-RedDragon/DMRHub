// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	gorm_seeder "github.com/kachit/gorm-seeder"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type User struct {
	ID         uint           `json:"id" gorm:"primaryKey" binding:"required"`
	Callsign   string         `json:"callsign" gorm:"uniqueIndex" binding:"required"`
	Username   string         `json:"username" gorm:"uniqueIndex" binding:"required"`
	Password   string         `json:"-"`
	Admin      bool           `json:"admin"`
	SuperAdmin bool           `json:"superAdmin"`
	Email      string         `json:"email"`
	Approved   bool           `json:"approved" binding:"required"`
	Suspended  bool           `json:"suspended"`
	Repeaters  []Repeater     `json:"repeaters" gorm:"foreignKey:OwnerID"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"-"`
	DeletedAt  gorm.DeletedAt `json:"-" gorm:"index"`
}

func (u User) TableName() string {
	return "users"
}

func UserExists(db *gorm.DB, user User) (bool, error) {
	var count int64
	err := db.Model(&User{}).Where("ID = ?", user.ID).Limit(1).Count(&count).Error
	return count > 0, err
}

func UserIDExists(db *gorm.DB, id uint) (bool, error) {
	var count int64
	err := db.Model(&User{}).Where("ID = ?", id).Limit(1).Count(&count).Error
	return count > 0, err
}

func FindUserByID(db *gorm.DB, id uint) (User, error) {
	var user User
	err := db.Preload("Repeaters").First(&user, id).Error
	return user, err
}

func ListUsers(db *gorm.DB) ([]User, error) {
	var users []User
	err := db.Preload("Repeaters").Find(&users).Error
	return users, err
}

func CountUsers(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&User{}).Count(&count).Error
	return int(count), err
}

func FindUserAdmins(db *gorm.DB) ([]User, error) {
	var users []User
	err := db.Preload("Repeaters").Where("admin = ?", true).Find(&users).Error
	return users, err
}

func CountUserAdmins(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&User{}).Where("admin = ?", true).Count(&count).Error
	return int(count), err
}

func FindUserSuspended(db *gorm.DB) ([]User, error) {
	var users []User
	err := db.Preload("Repeaters").Where("suspended = ?", true).Find(&users).Error
	return users, err
}

func CountUserSuspended(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&User{}).Where("suspended = ?", true).Count(&count).Error
	return int(count), err
}

func FindUserUnapproved(db *gorm.DB) ([]User, error) {
	var users []User
	err := db.Preload("Repeaters").Where("approved = ?", false).Find(&users).Error
	return users, err
}

func CountUserUnapproved(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&User{}).Where("approved = ?", false).Count(&count).Error
	return int(count), err
}

type UsersSeeder struct {
	gorm_seeder.SeederAbstract
}

const UserSeederRows = 1

func NewUsersSeeder(cfg gorm_seeder.SeederConfiguration) UsersSeeder {
	return UsersSeeder{gorm_seeder.NewSeederAbstract(cfg)}
}

func (s *UsersSeeder) Seed(db *gorm.DB) error {
	var users = []User{
		{
			ID:       dmrconst.ParrotUser,
			Callsign: "Parrot",
			Admin:    false,
			Approved: true,
		},
	}
	return db.CreateInBatches(users, s.Configuration.Rows).Error
}

func (s *UsersSeeder) Clear(_ *gorm.DB) error {
	return nil
}

func DeleteUser(db *gorm.DB, id uint) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		var repeaters []Repeater
		tx.Where("owner_id = ?", id).Find(&repeaters)
		for _, repeater := range repeaters {
			tx.Unscoped().Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, repeater.ID, repeater.ID).Delete(&Call{})
			tx.Unscoped().Select(clause.Associations, "TS1StaticTalkgroups").Select(clause.Associations, "TS2StaticTalkgroups").Delete(repeater)
			tx.Unscoped().Table("talkgroup_admins").Where("user_id = ?", id).Delete(&Talkgroup{})
			tx.Unscoped().Table("talkgroup_ncos").Where("user_id = ?", id).Delete(&Talkgroup{})
		}
		tx.Unscoped().Select(clause.Associations, "Repeaters").Delete(&User{ID: id})
		return nil
	})
	if err != nil {
		slog.Error("Error deleting user", "error", err)
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}
