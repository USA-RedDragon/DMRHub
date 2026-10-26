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

// drop_app_settings_migration_202610261100 drops the app settings table,
// which holds nothing that the hub reads.
func drop_app_settings_migration_202610261100(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type AppSettings struct {
		ID        uint `gorm:"primaryKey"`
		CreatedAt time.Time
		UpdatedAt time.Time
		DeletedAt gorm.DeletedAt `gorm:"index"`
	}

	return &gormigrate.Migration{
		ID: "202610261100",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&AppSettings{}) {
				return nil
			}
			if err := m.DropTable(&AppSettings{}); err != nil {
				return fmt.Errorf("could not drop app_settings: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().AutoMigrate(&AppSettings{}); err != nil {
				return fmt.Errorf("could not add app_settings: %w", err)
			}
			return nil
		},
	}
}
