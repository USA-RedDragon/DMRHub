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

type User struct {
	ID       uint   `json:"id" gorm:"primaryKey" binding:"required"`
	Callsign string `json:"callsign" gorm:"index:,unique" binding:"required"`
	Username string `json:"username" gorm:"index:,unique" binding:"required"`
	// Password is the hash of the password. Only SetPassword changes it,
	// so a Save of a stale User cannot undo a new password.
	Password   string `json:"-" gorm:"<-:create"`
	Admin      bool   `json:"admin"`
	SuperAdmin bool   `json:"superAdmin"`
	// Email is the email address of the user. Only SetEmail changes it
	// after the user is made, and it is not shown in lists of users.
	Email string `json:"-" gorm:"<-:create"`
	// EmailVerified reports that the user used the link sent to Email.
	// Only SetEmail and VerifyEmail write it.
	EmailVerified bool       `json:"-" gorm:"<-:create;not null;default:false"`
	Approved      bool       `json:"approved" binding:"required"`
	Suspended     bool       `json:"suspended"`
	Repeaters     []Repeater `json:"repeaters" gorm:"foreignKey:OwnerID"`
	// SMSStorageEnabled reports that the user chose to see the stored
	// messages of the radio IDs that they own. Only SetSMSStorage writes
	// it, so a Save of a stale User cannot undo a change.
	SMSStorageEnabled bool `json:"-" gorm:"<-:create;not null;default:false"`
	// SMSStorageSince is when the user last turned SMS Storage on.
	SMSStorageSince *time.Time `json:"-" gorm:"<-:create"`
	// SharePositions reports that the user shows the positions of their
	// radios to every other user. Only SetSharePositions
	// writes it.
	SharePositions bool `json:"-" gorm:"<-:create;not null;default:false"`
	// MustChangePassword reports that an admin set the password of the
	// user, who must choose their own before they use the rest of
	// DMRHub. Only SetPassword writes it.
	MustChangePassword bool           `json:"must_change_password" gorm:"<-:create;not null;default:false"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"-"`
	DeletedAt          gorm.DeletedAt `json:"-" gorm:"index"`
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

// FindUserWithoutRepeaters returns the user id without its repeaters.
func FindUserWithoutRepeaters(db *gorm.DB, id uint) (User, error) {
	var user User
	err := db.First(&user, id).Error
	return user, err
}

// FindUserWithRepeaterTalkgroups returns the user with its repeaters and
// their static talkgroups, which a repeater needs to tell which calls it
// receives.
func FindUserWithRepeaterTalkgroups(db *gorm.DB, id uint) (User, error) {
	var user User
	err := db.Preload("Repeaters.TS1StaticTalkgroups").Preload("Repeaters.TS2StaticTalkgroups").First(&user, id).Error
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

// DeleteUser deletes the user id and what they own in one transaction.
// Their repeaters go with the calls through them, their peers with their
// rules. Their sessions, app grants, password reset links and tracked
// positions end. Their other calls, nets and scheduled nets stay without
// the user.
func DeleteUser(db *gorm.DB, id uint) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		var repeaters []Repeater
		if err := tx.Unscoped().Where("owner_id = ?", id).Find(&repeaters).Error; err != nil {
			return fmt.Errorf("could not list the repeaters: %w", err)
		}
		for i := range repeaters {
			r := &repeaters[i]
			if err := tx.Unscoped().Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, r.ID, r.ID).Delete(&Call{}).Error; err != nil {
				return fmt.Errorf("could not delete the calls of repeater %d: %w", r.ID, err)
			}
			if err := tx.Unscoped().Select(clause.Associations, "TS1StaticTalkgroups").Select(clause.Associations, "TS2StaticTalkgroups").Delete(r).Error; err != nil {
				return fmt.Errorf("could not delete repeater %d: %w", r.ID, err)
			}
		}
		var peers []uint
		if err := tx.Unscoped().Model(&Peer{}).Where("owner_id = ?", id).Pluck("id", &peers).Error; err != nil {
			return fmt.Errorf("could not list the peers: %w", err)
		}
		for _, peer := range peers {
			if err := DeletePeer(tx, peer); err != nil {
				return err
			}
		}
		for _, ref := range []struct {
			model  any
			column string
		}{
			{&Call{}, "user_id"},
			{&Call{}, "to_user_id"},
			{&Net{}, colStartedByUserID},
			{&ScheduledNet{}, colCreatedByUserID},
		} {
			if err := tx.Unscoped().Model(ref.model).Where(ref.column+" = ?", id).UpdateColumn(ref.column, nil).Error; err != nil {
				return fmt.Errorf("could not clear %s: %w", ref.column, err)
			}
		}
		tracked := tx.Model(&PositionSetting{}).Select("radio_id").Where("user_id = ?", id)
		if err := tx.Where("radio_id IN (?)", tracked).Delete(&Position{}).Error; err != nil {
			return fmt.Errorf("could not delete the positions: %w", err)
		}
		grants := tx.Model(&OAuthGrant{}).Select("id").Where("user_id = ?", id)
		if err := tx.Where("grant_id IN (?)", grants).Delete(&OAuthRefreshToken{}).Error; err != nil {
			return fmt.Errorf("could not delete the refresh tokens: %w", err)
		}
		for _, model := range []any{&PositionSetting{}, &OAuthGrant{}, &OAuthCode{}, &PasswordReset{}, &Session{}} {
			if err := tx.Where("user_id = ?", id).Delete(model).Error; err != nil {
				return fmt.Errorf("could not delete the %T rows: %w", model, err)
			}
		}
		for _, table := range []string{"talkgroup_admins", "talkgroup_ncos", "user_radio_ids"} {
			if err := tx.Exec("DELETE FROM "+table+" WHERE user_id = ?", id).Error; err != nil {
				return fmt.Errorf("could not delete from %s: %w", table, err)
			}
		}
		if err := tx.Unscoped().Delete(&User{ID: id}).Error; err != nil {
			return fmt.Errorf("could not delete the user: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to delete user %d: %w", id, err)
	}
	return nil
}
