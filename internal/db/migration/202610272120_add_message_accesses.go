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

// add_message_accesses_migration_202610272120 adds the log of the times
// that admins saw the stored messages of other users.
func add_message_accesses_migration_202610272120(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type MessageAccess struct {
		ID        uint      `gorm:"primaryKey"`
		At        time.Time `gorm:"not null;index"`
		AdminID   uint      `gorm:"not null;index"`
		Kind      string    `gorm:"size:8;not null"`
		MessageID *uint
		Filter    string `gorm:"size:512;not null;default:''"`
		Results   int    `gorm:"not null;default:0"`
	}
	type MessageAccessRadioID struct {
		AccessID uint `gorm:"primaryKey;autoIncrement:false"`
		RadioID  uint `gorm:"primaryKey;autoIncrement:false;index"`
	}

	return &gormigrate.Migration{
		ID: "202610272120",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable("message_accesses") {
				if err := m.CreateTable(&MessageAccess{}); err != nil {
					return fmt.Errorf("could not create the message_accesses table: %w", err)
				}
			}
			if !m.HasTable("message_access_radio_ids") {
				if err := m.CreateTable(&MessageAccessRadioID{}); err != nil {
					return fmt.Errorf("could not create the message_access_radio_ids table: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if err := m.DropTable("message_access_radio_ids", "message_accesses"); err != nil {
				return fmt.Errorf("could not drop the message access tables: %w", err)
			}
			return nil
		},
	}
}
