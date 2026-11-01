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

// add_store_forward_migration_202611011810 adds the store and forward
// switch of each user and the table of the held texts.
func add_store_forward_migration_202611011810(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type User struct {
		ID           uint `gorm:"primaryKey"`
		StoreForward bool `gorm:"not null;default:false"`
	}
	type HeldMessage struct {
		ID        uint      `gorm:"primarykey"`
		MessageID uint      `gorm:"not null;uniqueIndex"`
		RadioID   uint      `gorm:"not null;index:idx_held_messages_radio_state,priority:1"`
		UserID    uint      `gorm:"not null;index:idx_held_messages_user_state,priority:1"`
		State     string    `gorm:"size:16;not null;index:idx_held_messages_radio_state,priority:2;index:idx_held_messages_user_state,priority:2"`
		Attempts  uint      `gorm:"not null;default:0"`
		HeldAt    time.Time `gorm:"not null;index"`
		DoneAt    *time.Time
		Response  string `gorm:"size:8"`
		ClaimedBy string `gorm:"size:64"`
		ClaimedAt *time.Time
	}

	return &gormigrate.Migration{
		ID: "202611011810",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasTable(&User{}) && !m.HasColumn(&User{}, "StoreForward") {
				if err := m.AddColumn(&User{}, "StoreForward"); err != nil {
					return fmt.Errorf("could not add store_forward to users: %w", err)
				}
			}
			if !m.HasTable(&HeldMessage{}) {
				if err := m.CreateTable(&HeldMessage{}); err != nil {
					return fmt.Errorf("could not create the held_messages table: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if err := m.DropTable(&HeldMessage{}); err != nil {
				return fmt.Errorf("could not drop the held_messages table: %w", err)
			}
			if m.HasColumn(&User{}, "StoreForward") {
				if err := m.DropColumn(&User{}, "StoreForward"); err != nil {
					return fmt.Errorf("could not drop store_forward from users: %w", err)
				}
			}
			return nil
		},
	}
}
