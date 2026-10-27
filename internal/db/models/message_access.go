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
	"slices"
	"time"

	"gorm.io/gorm"
)

// MessageAccessKind is how an admin saw stored messages.
type MessageAccessKind string

const (
	// MessageAccessSearch is a search of the stored messages.
	MessageAccessSearch MessageAccessKind = "search"
	// MessageAccessRead is a read of one stored message with its raw
	// octets.
	MessageAccessRead MessageAccessKind = "read"
)

// MessageAccess is one time that an admin saw the stored messages of other
// users: who, when, how, and the radio IDs whose messages the answer held.
type MessageAccess struct {
	ID        uint              `gorm:"primaryKey"`
	At        time.Time         `gorm:"not null;index"`
	AdminID   uint              `gorm:"not null;index"`
	Kind      MessageAccessKind `gorm:"size:8;not null"`
	MessageID *uint
	Filter    string `gorm:"size:512;not null;default:''"`
	Results   int    `gorm:"not null;default:0"`
}

func (MessageAccess) TableName() string {
	return "message_accesses"
}

// MessageAccessRadioID is a radio ID whose messages an admin saw in one
// MessageAccess, as the source or the destination of a message.
type MessageAccessRadioID struct {
	AccessID uint `gorm:"primaryKey;autoIncrement:false"`
	RadioID  uint `gorm:"primaryKey;autoIncrement:false;index"`
}

func (MessageAccessRadioID) TableName() string {
	return "message_access_radio_ids"
}

// LogMessageAccess stores that the user admin saw msgs at the time at, with the
// radio IDs of their sources and destinations.
func LogMessageAccess(db *gorm.DB, admin uint, kind MessageAccessKind, messageID *uint, filter string, msgs []Message, at time.Time) error {
	if len(filter) > 512 {
		filter = filter[:512]
	}
	row := MessageAccess{At: at.UTC(), AdminID: admin, Kind: kind, MessageID: messageID, Filter: filter, Results: len(msgs)}
	var ids []uint
	for _, m := range msgs {
		ids = append(ids, m.SourceID, m.DestinationID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("could not log a message access: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		rows := make([]MessageAccessRadioID, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, MessageAccessRadioID{AccessID: row.ID, RadioID: id})
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("could not log the radio IDs of a message access: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("message access: %w", err)
	}
	return nil
}

// MessageAccessQuery picks message accesses: of the admin AdminID, and of
// the messages of RadioID, when not 0.
type MessageAccessQuery struct {
	AdminID uint
	RadioID uint
}

func (q MessageAccessQuery) scope(db *gorm.DB) *gorm.DB {
	db = db.Model(&MessageAccess{})
	if q.AdminID != 0 {
		db = db.Where("admin_id = ?", q.AdminID)
	}
	if q.RadioID != 0 {
		db = db.Where("id IN (?)", db.Session(&gorm.Session{NewDB: true}).Model(&MessageAccessRadioID{}).Select("access_id").Where("radio_id = ?", q.RadioID))
	}
	return db
}

// ListMessageAccesses returns a page of the message accesses that q picks,
// newest first, with the radio IDs of each by access ID, and how many q
// picks in all.
func ListMessageAccesses(db *gorm.DB, q MessageAccessQuery, offset, limit int) ([]MessageAccess, map[uint][]uint, int64, error) {
	var total int64
	if err := q.scope(db).Count(&total).Error; err != nil {
		return nil, nil, 0, fmt.Errorf("could not count the message accesses: %w", err)
	}
	var rows []MessageAccess
	if err := q.scope(db).Order("at DESC").Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, 0, fmt.Errorf("could not list the message accesses: %w", err)
	}
	ids := make(map[uint][]uint, len(rows))
	if len(rows) == 0 {
		return rows, ids, total, nil
	}
	accessIDs := make([]uint, 0, len(rows))
	for _, r := range rows {
		accessIDs = append(accessIDs, r.ID)
	}
	var links []MessageAccessRadioID
	if err := db.Where("access_id IN ?", accessIDs).Order("radio_id").Find(&links).Error; err != nil {
		return nil, nil, 0, fmt.Errorf("could not list the radio IDs of the message accesses: %w", err)
	}
	for _, l := range links {
		ids[l.AccessID] = append(ids[l.AccessID], l.RadioID)
	}
	return rows, ids, total, nil
}
