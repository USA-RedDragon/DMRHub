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

// add_setup_settings_migration_202610261500 adds the table of the settings
// that admins save in Setup, which every replica shares.
func add_setup_settings_migration_202610261500(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type SetupSettings struct {
		ID        uint   `gorm:"primaryKey;autoIncrement:false"`
		Revision  uint64 `gorm:"not null;default:0"`
		Settings  string `gorm:"type:text;not null"`
		UpdatedAt time.Time
	}

	return &gormigrate.Migration{
		ID: "202610261500",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasTable(&SetupSettings{}) {
				return nil
			}
			if err := m.CreateTable(&SetupSettings{}); err != nil {
				return fmt.Errorf("could not create the setup_settings table: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable(&SetupSettings{}); err != nil {
				return fmt.Errorf("could not drop the setup_settings table: %w", err)
			}
			return nil
		},
	}
}
