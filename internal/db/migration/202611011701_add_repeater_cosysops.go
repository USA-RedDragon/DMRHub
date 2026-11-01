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

func add_repeater_cosysops_migration_202611011701(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type RepeaterCosysop struct {
		RepeaterID uint      `gorm:"primaryKey;autoIncrement:false"`
		UserID     uint      `gorm:"primaryKey;autoIncrement:false;index"`
		InvitedBy  uint      `gorm:"not null"`
		InvitedAt  time.Time `gorm:"not null"`
		ExpiresAt  time.Time `gorm:"not null"`
		AcceptedAt *time.Time
	}

	return &gormigrate.Migration{
		ID: "202611011701",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasTable("repeater_cosysops") {
				return nil
			}
			if err := m.CreateTable(&RepeaterCosysop{}); err != nil {
				return fmt.Errorf("could not create the repeater_cosysops table: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable("repeater_cosysops"); err != nil {
				return fmt.Errorf("could not drop the repeater_cosysops table: %w", err)
			}
			return nil
		},
	}
}
