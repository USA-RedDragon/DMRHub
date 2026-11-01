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
)

// The states of a repeater in a local group.
const (
	// LocalMemberIn is a repeater that takes the calls of the group.
	LocalMemberIn = "member"
	// LocalMemberAsked is a repeater that the group owner asked to add,
	// which waits for its owner to say yes.
	LocalMemberAsked = "asked"
)

// ErrLocalNumberTaken is the error for a repeater that is already in, or
// asked to join, another local group on the same talkgroup number.
var ErrLocalNumberTaken = errors.New("the repeater is already in a local group on that talkgroup")

// LocalGroup is a group of repeaters that share a local talkgroup: a group
// call to its talkgroup on the set timeslot of a member repeater goes to
// the other members and nowhere else.
type LocalGroup struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"size:64;not null"`
	Talkgroup uint   `gorm:"not null"`
	OwnerID   uint   `gorm:"not null;index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LocalGroupMember is a repeater in a local group, or asked to join it.
// Talkgroup repeats the talkgroup of the group, so that the database
// keeps a repeater in one group for each talkgroup number. Slot is 1 or 2.
type LocalGroupMember struct {
	LocalGroupID uint   `gorm:"primaryKey;autoIncrement:false"`
	RepeaterID   uint   `gorm:"primaryKey;autoIncrement:false;uniqueIndex:idx_local_group_members_number,priority:1"`
	Talkgroup    uint   `gorm:"not null;uniqueIndex:idx_local_group_members_number,priority:2"`
	Slot         uint8  `gorm:"not null"`
	State        string `gorm:"size:8;not null"`
	AskedByID    uint   `gorm:"not null;default:0"`
	CreatedAt    time.Time
}

// LocalMembership is a local group that a repeater is in, as routing reads
// it. Slot is true for TS2.
type LocalMembership struct {
	GroupID   uint
	Name      string
	Talkgroup uint
	Slot      bool
}

// LocalGroupOn returns the local group of the repeater on the talkgroup
// tg, whatever its slot.
func (p *Repeater) LocalGroupOn(tg uint) (LocalMembership, bool) {
	for _, m := range p.LocalGroups {
		if m.Talkgroup == tg {
			return m, true
		}
	}
	return LocalMembership{}, false
}

// LocalGroupByID returns the membership of the repeater in the local group
// id.
func (p *Repeater) LocalGroupByID(id uint) (LocalMembership, bool) {
	for _, m := range p.LocalGroups {
		if m.GroupID == id {
			return m, true
		}
	}
	return LocalMembership{}, false
}

// RepeaterLocalGroups returns the local groups that the repeater is in.
func RepeaterLocalGroups(db *gorm.DB, repeaterID uint) ([]LocalMembership, error) {
	var rows []struct {
		GroupID   uint
		Name      string
		Talkgroup uint
		Slot      uint8
	}
	err := db.Table("local_group_members AS m").
		Select("m.local_group_id AS group_id, g.name AS name, g.talkgroup AS talkgroup, m.slot AS slot").
		Joins("JOIN local_groups AS g ON g.id = m.local_group_id").
		Where("m.repeater_id = ? AND m.state = ?", repeaterID, LocalMemberIn).
		Order("g.talkgroup").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("could not read the local groups of repeater %d: %w", repeaterID, err)
	}
	out := make([]LocalMembership, 0, len(rows))
	for _, r := range rows {
		out = append(out, LocalMembership{GroupID: r.GroupID, Name: r.Name, Talkgroup: r.Talkgroup, Slot: r.Slot == 2})
	}
	return out, nil
}

// LocalGroupRepeaterIDs returns the repeaters that are in the local group
// id, without those only asked to join.
func LocalGroupRepeaterIDs(db *gorm.DB, id uint) ([]uint, error) {
	var ids []uint
	err := db.Model(&LocalGroupMember{}).Where("local_group_id = ? AND state = ?", id, LocalMemberIn).Order("repeater_id").Pluck("repeater_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the repeaters of local group %d: %w", id, err)
	}
	return ids, nil
}

// LocalGroupMembers returns the repeaters in the local group id and those
// asked to join it.
func LocalGroupMembers(db *gorm.DB, id uint) ([]LocalGroupMember, error) {
	var members []LocalGroupMember
	if err := db.Where("local_group_id = ?", id).Order("created_at, repeater_id").Find(&members).Error; err != nil {
		return nil, fmt.Errorf("could not list the repeaters of local group %d: %w", id, err)
	}
	return members, nil
}

// FindLocalGroup returns the local group id.
func FindLocalGroup(db *gorm.DB, id uint) (LocalGroup, error) {
	var g LocalGroup
	if err := db.First(&g, id).Error; err != nil {
		return g, fmt.Errorf("could not find local group %d: %w", id, err)
	}
	return g, nil
}

// numberTaken reports whether the repeater is in, or asked to join, a local
// group other than group on the talkgroup tg.
func numberTaken(tx *gorm.DB, repeaterID, tg, group uint) (bool, error) {
	var n int64
	err := tx.Model(&LocalGroupMember{}).Where("repeater_id = ? AND talkgroup = ? AND local_group_id <> ?", repeaterID, tg, group).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("could not check the local groups of repeater %d: %w", repeaterID, err)
	}
	return n > 0, nil
}

// AddLocalGroupMember adds m to its local group, with the talkgroup of the
// group. It returns ErrLocalNumberTaken when the repeater is in, or asked to
// join, another local group on that talkgroup, and gorm.ErrDuplicatedKey
// when it is already in this one.
func AddLocalGroupMember(db *gorm.DB, m LocalGroupMember) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		g, err := FindLocalGroup(tx, m.LocalGroupID)
		if err != nil {
			return err
		}
		m.Talkgroup = g.Talkgroup
		var n int64
		if err := tx.Model(&LocalGroupMember{}).Where("local_group_id = ? AND repeater_id = ?", m.LocalGroupID, m.RepeaterID).Count(&n).Error; err != nil {
			return fmt.Errorf("could not check local group %d: %w", m.LocalGroupID, err)
		}
		if n > 0 {
			return gorm.ErrDuplicatedKey
		}
		taken, err := numberTaken(tx, m.RepeaterID, m.Talkgroup, m.LocalGroupID)
		if err != nil {
			return err
		}
		if taken {
			return ErrLocalNumberTaken
		}
		if err := tx.Create(&m).Error; err != nil {
			return fmt.Errorf("could not add repeater %d to local group %d: %w", m.RepeaterID, m.LocalGroupID, err)
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrLocalNumberTaken) && !errors.Is(err, gorm.ErrDuplicatedKey) {
		if taken, cerr := numberTaken(db, m.RepeaterID, m.Talkgroup, m.LocalGroupID); cerr == nil && taken {
			return ErrLocalNumberTaken
		}
	}
	return err //nolint:wrapcheck // the transaction returns wrapped errors
}

// UpdateLocalGroup renames the local group id and moves it and its
// repeaters to the talkgroup tg. It returns ErrLocalNumberTaken when one of
// its repeaters is in another local group on tg.
func UpdateLocalGroup(db *gorm.DB, id uint, name string, tg uint) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		var members []uint
		if err := tx.Model(&LocalGroupMember{}).Where("local_group_id = ?", id).Pluck("repeater_id", &members).Error; err != nil {
			return fmt.Errorf("could not list the repeaters of local group %d: %w", id, err)
		}
		for _, r := range members {
			taken, err := numberTaken(tx, r, tg, id)
			if err != nil {
				return err
			}
			if taken {
				return ErrLocalNumberTaken
			}
		}
		if _, err := FindLocalGroup(tx, id); err != nil {
			return err
		}
		if err := tx.Model(&LocalGroup{}).Where("id = ?", id).Updates(LocalGroup{Name: name, Talkgroup: tg}).Error; err != nil {
			return fmt.Errorf("could not save local group %d: %w", id, err)
		}
		if err := tx.Model(&LocalGroupMember{}).Where("local_group_id = ?", id).Update("talkgroup", tg).Error; err != nil {
			return fmt.Errorf("could not move the repeaters of local group %d: %w", id, err)
		}
		return nil
	})
	return err //nolint:wrapcheck // the transaction returns wrapped errors
}

// SetLocalGroupMember sets the slot, 1 or 2, of the repeater in the local
// group, and makes it a member when in is set.
func SetLocalGroupMember(db *gorm.DB, group, repeaterID uint, slot uint8, in bool) error {
	fields := map[string]any{}
	if slot != 0 {
		fields["slot"] = slot
	}
	if in {
		fields["state"] = LocalMemberIn
	}
	row := db.Model(&LocalGroupMember{}).Where("local_group_id = ? AND repeater_id = ?", group, repeaterID)
	var n int64
	if err := row.Session(&gorm.Session{}).Count(&n).Error; err != nil {
		return fmt.Errorf("could not find repeater %d in local group %d: %w", repeaterID, group, err)
	}
	if n == 0 {
		return gorm.ErrRecordNotFound
	}
	if len(fields) == 0 {
		return nil
	}
	if err := row.Updates(fields).Error; err != nil {
		return fmt.Errorf("could not change repeater %d in local group %d: %w", repeaterID, group, err)
	}
	return nil
}

// RemoveLocalGroupMember takes the repeater out of the local group, or
// takes back the request to add it.
func RemoveLocalGroupMember(db *gorm.DB, group, repeaterID uint) error {
	res := db.Where("local_group_id = ? AND repeater_id = ?", group, repeaterID).Delete(&LocalGroupMember{})
	if res.Error != nil {
		return fmt.Errorf("could not take repeater %d out of local group %d: %w", repeaterID, group, res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteLocalGroup deletes the local group id and returns the repeaters
// that were in it or asked to join it.
func DeleteLocalGroup(db *gorm.DB, id uint) ([]uint, error) {
	var members []uint
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&LocalGroupMember{}).Where("local_group_id = ?", id).Pluck("repeater_id", &members).Error; err != nil {
			return fmt.Errorf("could not list the repeaters of local group %d: %w", id, err)
		}
		if err := tx.Where("local_group_id = ?", id).Delete(&LocalGroupMember{}).Error; err != nil {
			return fmt.Errorf("could not delete the repeaters of local group %d: %w", id, err)
		}
		res := tx.Delete(&LocalGroup{}, id)
		if res.Error != nil {
			return fmt.Errorf("could not delete local group %d: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	return members, err //nolint:wrapcheck // the transaction returns wrapped errors
}

// LocalGroupsOwnedBy returns the IDs of the local groups that the user owns.
func LocalGroupsOwnedBy(db *gorm.DB, userID uint) ([]uint, error) {
	var ids []uint
	if err := db.Model(&LocalGroup{}).Where("owner_id = ?", userID).Order("id").Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("could not list the local groups of user %d: %w", userID, err)
	}
	return ids, nil
}

// LocalGroupsWithRepeatersOf returns the IDs of the local groups that a
// repeater of the user is in or asked to join.
func LocalGroupsWithRepeatersOf(db *gorm.DB, userID uint) ([]uint, error) {
	var ids []uint
	err := db.Model(&LocalGroupMember{}).Distinct("local_group_id").
		Where("repeater_id IN (?)", db.Model(&Repeater{}).Select("id").Where("owner_id = ?", userID)).
		Pluck("local_group_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the local groups of the repeaters of user %d: %w", userID, err)
	}
	return ids, nil
}

// LocalGroupsOfRepeater returns the rows of the repeater in local groups,
// both those it is in and those it is asked to join.
func LocalGroupsOfRepeater(db *gorm.DB, repeaterID uint) ([]LocalGroupMember, error) {
	var rows []LocalGroupMember
	if err := db.Where("repeater_id = ?", repeaterID).Order("talkgroup").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("could not list the local groups of repeater %d: %w", repeaterID, err)
	}
	return rows, nil
}

// deleteLocalGroupsOf deletes the rows of the repeater in local groups.
func deleteLocalGroupsOf(tx *gorm.DB, repeaterID uint) error {
	if err := tx.Where("repeater_id = ?", repeaterID).Delete(&LocalGroupMember{}).Error; err != nil {
		return fmt.Errorf("could not take repeater %d out of its local groups: %w", repeaterID, err)
	}
	return nil
}

// deleteLocalGroupsOwnedBy deletes the local groups of the user and their
// rows.
func deleteLocalGroupsOwnedBy(tx *gorm.DB, userID uint) error {
	owned := tx.Model(&LocalGroup{}).Select("id").Where("owner_id = ?", userID)
	if err := tx.Where("local_group_id IN (?)", owned).Delete(&LocalGroupMember{}).Error; err != nil {
		return fmt.Errorf("could not delete the repeaters of the local groups of user %d: %w", userID, err)
	}
	if err := tx.Where("owner_id = ?", userID).Delete(&LocalGroup{}).Error; err != nil {
		return fmt.Errorf("could not delete the local groups of user %d: %w", userID, err)
	}
	return nil
}
