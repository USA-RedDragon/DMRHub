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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func add_simplex_repeater_column_migration_202602130100(db *gorm.DB, _ *config.Config) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202602130100",
		Migrate: func(tx *gorm.DB) error {
			if db.Migrator().HasTable(&models.Repeater{}) && !db.Migrator().HasColumn(&models.Repeater{}, "simplex_repeater") {
				err := tx.Migrator().AddColumn(&models.Repeater{}, "SimplexRepeater")
				if err != nil {
					return fmt.Errorf("could not add simplex_repeater column: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if db.Migrator().HasTable(&models.Repeater{}) && db.Migrator().HasColumn(&models.Repeater{}, "simplex_repeater") {
				err := tx.Migrator().DropColumn(&models.Repeater{}, "SimplexRepeater")
				if err != nil {
					return fmt.Errorf("could not drop simplex_repeater column: %w", err)
				}
			}
			return nil
		},
	}
}
