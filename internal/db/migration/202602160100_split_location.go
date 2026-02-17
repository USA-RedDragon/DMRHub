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
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func split_location_migration_202602160100(db *gorm.DB, cfg *config.Config) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202602160100",
		Migrate: func(tx *gorm.DB) error {
			if !db.Migrator().HasTable(&models.Repeater{}) {
				return nil
			}

			// Add city, state, country columns if they don't exist
			for _, col := range []struct {
				name  string
				field string
			}{
				{"city", "City"},
				{"state", "State"},
				{"country", "Country"},
			} {
				if !db.Migrator().HasColumn(&models.Repeater{}, col.name) {
					if err := tx.Migrator().AddColumn(&models.Repeater{}, col.field); err != nil {
						return fmt.Errorf("could not add %s column: %w", col.name, err)
					}
				}
			}

			// Skip data population if radio ID validation is disabled
			if cfg.DMR.DisableRadioIDValidation {
				return nil
			}

			// Fetch all repeaters with 6-digit IDs (real repeaters, not hotspots)
			var repeaters []models.Repeater
			if err := tx.Where("id >= 100000 AND id <= 999999").Find(&repeaters).Error; err != nil {
				return fmt.Errorf("could not fetch repeaters: %w", err)
			}

			for i := range repeaters {
				r, ok := repeaterdb.Get(repeaters[i].ID)
				if !ok {
					slog.Warn("Repeater not found in repeaterdb during migration, skipping",
						"repeater_id", repeaters[i].ID)
					continue
				}

				repeaters[i].City = r.City
				repeaters[i].State = r.State
				repeaters[i].Country = r.Country

				if err := tx.Save(&repeaters[i]).Error; err != nil {
					return fmt.Errorf("could not update repeater %d: %w", repeaters[i].ID, err)
				}
			}

			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if !db.Migrator().HasTable(&models.Repeater{}) {
				return nil
			}
			for _, col := range []string{"City", "State", "Country"} {
				if db.Migrator().HasColumn(&models.Repeater{}, col) {
					if err := tx.Migrator().DropColumn(&models.Repeater{}, col); err != nil {
						return fmt.Errorf("could not drop %s column: %w", col, err)
					}
				}
			}
			return nil
		},
	}
}
