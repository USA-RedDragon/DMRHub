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

func fix_call_data_migration_202602100457(db *gorm.DB, _ *config.Config) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202602100457",
		Migrate: func(tx *gorm.DB) error {
			switch tx.Name() {
			case "postgres":
				if db.Migrator().HasTable(&models.Call{}) {
					if err := tx.Exec("ALTER TABLE calls DROP COLUMN IF EXISTS call_data").Error; err != nil {
						return fmt.Errorf("could not drop call_data column: %w", err)
					}
					if err := tx.Exec("ALTER TABLE calls ADD COLUMN call_data bytea DEFAULT NULL").Error; err != nil {
						return fmt.Errorf("could not add call_data column: %w", err)
					}
				}
			case "mysql":
				// MySQL uses LONGBLOB for []byte
				if db.Migrator().HasTable(&models.Call{}) && db.Migrator().HasColumn(&models.Call{}, "call_data") {
					if err := tx.Exec("ALTER TABLE calls MODIFY COLUMN call_data LONGBLOB").Error; err != nil {
						return fmt.Errorf("could not modify call_data column: %w", err)
					}
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			return nil
		},
	}
}
