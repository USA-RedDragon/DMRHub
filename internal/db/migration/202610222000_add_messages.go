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

package migration

import (
	"fmt"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func add_messages_migration_202610222000(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type User struct {
		ID                uint `gorm:"primaryKey"`
		SMSStorageEnabled bool `gorm:"not null;default:false"`
		SMSStorageSince   *time.Time
	}
	type Call struct {
		ID uint `gorm:"primarykey"`
	}
	type Message struct {
		ID             uint      `gorm:"primarykey"`
		ReceivedAt     time.Time `gorm:"not null;index;index:idx_messages_destination_received,priority:2;index:idx_messages_source_received,priority:2"`
		TransmissionID *uint     `gorm:"index"`
		Transmission   *Call     `gorm:"foreignKey:TransmissionID;constraint:OnDelete:SET NULL"`
		SourceID       uint      `gorm:"not null;index:idx_messages_source_received,priority:1"`
		DestinationID  uint      `gorm:"not null;index:idx_messages_destination_received,priority:1"`
		IsGroup        bool      `gorm:"not null;default:false"`
		RepeaterID     uint      `gorm:"not null;default:0"`
		Slot           uint8     `gorm:"not null;default:1"`
		Transport      string    `gorm:"size:16"`
		StreamID       uint      `gorm:"not null;default:0"`
		SAP            *uint8
		Format         string `gorm:"size:16;not null;index"`
		Confirmed      bool   `gorm:"not null;default:false"`
		SendSequence   *uint8
		FragmentSeq    *uint8
		Response       string  `gorm:"size:8"`
		BadCRC         bool    `gorm:"not null;default:false"`
		Text           *string `gorm:"type:text"`
		ChecksumOK     bool    `gorm:"not null"`
		Blocks         uint    `gorm:"not null;default:0"`
		Octets         uint    `gorm:"not null;default:0"`
		Raw            []byte
		CreatedAt      time.Time
		UpdatedAt      time.Time
	}
	type UserRadioID struct {
		ID         uint      `gorm:"primarykey"`
		UserID     uint      `gorm:"not null;index"`
		User       *User     `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
		RadioID    uint      `gorm:"not null;uniqueIndex"`
		VerifiedAt time.Time `gorm:"not null"`
		CreatedAt  time.Time
	}

	return &gormigrate.Migration{
		ID: "202610222000",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&User{}) || !m.HasTable(&Call{}) {
				return nil
			}
			for _, col := range []string{"SMSStorageEnabled", "SMSStorageSince"} {
				if m.HasColumn(&User{}, col) {
					continue
				}
				if err := m.AddColumn(&User{}, col); err != nil {
					return fmt.Errorf("could not add %s column to users: %w", col, err)
				}
			}
			if !m.HasTable(&Message{}) {
				if err := m.CreateTable(&Message{}); err != nil {
					return fmt.Errorf("could not create the messages table: %w", err)
				}
			}
			if !m.HasTable(&UserRadioID{}) {
				if err := m.CreateTable(&UserRadioID{}); err != nil {
					return fmt.Errorf("could not create the user_radio_ids table: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, table := range []any{&UserRadioID{}, &Message{}} {
				if err := m.DropTable(table); err != nil {
					return fmt.Errorf("could not drop table: %w", err)
				}
			}
			for _, col := range []string{"SMSStorageEnabled", "SMSStorageSince"} {
				if !m.HasColumn(&User{}, col) {
					continue
				}
				if err := m.DropColumn(&User{}, col); err != nil {
					return fmt.Errorf("could not drop %s column from users: %w", col, err)
				}
			}
			return nil
		},
	}
}
