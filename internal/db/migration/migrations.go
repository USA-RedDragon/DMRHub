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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, migrations(db))

	if err := m.Migrate(); err != nil {
		return fmt.Errorf("could not migrate: %w", err)
	}

	return nil
}

func migrations(db *gorm.DB) []*gormigrate.Migration {
	m := schemaMigrations(db)
	m = append(m, dataMigrations(db)...)
	return m
}

func schemaMigrations(db *gorm.DB) []*gormigrate.Migration {
	return []*gormigrate.Migration{
		// convert models.Repeater radio_id to id
		{
			ID: "202302242025",
			Migrate: func(tx *gorm.DB) error {
				if db.Migrator().HasTable(&models.Repeater{}) && db.Migrator().HasColumn(&models.Repeater{}, "radio_id") {
					err := tx.Migrator().RenameColumn(&models.Repeater{}, "radio_id", "id")
					if err != nil {
						return fmt.Errorf("could not rename column: %w", err)
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				if db.Migrator().HasTable(&models.Repeater{}) && db.Migrator().HasColumn(&models.Repeater{}, "id") && !db.Migrator().HasColumn(&models.Repeater{}, "radio_id") {
					err := tx.Migrator().RenameColumn(&models.Repeater{}, "id", "radio_id")
					if err != nil {
						return fmt.Errorf("could not rename column: %w", err)
					}
				}
				return nil
			},
		},
		{
			ID: "202311190252",
			Migrate: func(tx *gorm.DB) error {
				if db.Migrator().HasTable("repeater_ts1_static_talkgroups") && db.Migrator().HasColumn("repeater_ts1_static_talkgroups", "repeater_radio_id") {
					err := tx.Migrator().DropTable("repeater_ts1_static_talkgroups")
					if err != nil {
						return fmt.Errorf("could not drop table: %w", err)
					}
				}
				if db.Migrator().HasTable("repeater_ts2_static_talkgroups") && db.Migrator().HasColumn("repeater_ts2_static_talkgroups", "repeater_radio_id") {
					err := tx.Migrator().DropTable("repeater_ts2_static_talkgroups")
					if err != nil {
						return fmt.Errorf("could not drop table: %w", err)
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		// Fix call_data column type from bigint to bytea
		{
			ID: "202602100435",
			Migrate: func(tx *gorm.DB) error {
				if db.Migrator().HasTable(&models.Call{}) && db.Migrator().HasColumn(&models.Call{}, "call_data") {
					err := tx.Migrator().DropColumn(&models.Call{}, "call_data")
					if err != nil {
						return fmt.Errorf("could not drop column: %w", err)
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
	}
}

func dataMigrations(db *gorm.DB) []*gormigrate.Migration {
	return []*gormigrate.Migration{
		// Fix call_data column type using raw SQL (previous migration may not have worked)
		{
			ID: "202602100457",
			Migrate: func(tx *gorm.DB) error {
				switch tx.Name() {
				case "postgres":
					if err := tx.Exec("ALTER TABLE calls DROP COLUMN IF EXISTS call_data").Error; err != nil {
						return fmt.Errorf("could not drop call_data column: %w", err)
					}
					if err := tx.Exec("ALTER TABLE calls ADD COLUMN call_data bytea DEFAULT NULL").Error; err != nil {
						return fmt.Errorf("could not add call_data column: %w", err)
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
		},
		// Add type column to repeaters for MMDVM vs IPSC differentiation
		{
			ID: "202506150100",
			Migrate: func(tx *gorm.DB) error {
				if db.Migrator().HasTable(&models.Repeater{}) && !db.Migrator().HasColumn(&models.Repeater{}, "type") {
					err := tx.Migrator().AddColumn(&models.Repeater{}, "Type")
					if err != nil {
						return fmt.Errorf("could not add type column: %w", err)
					}
					// Set existing repeaters to mmdvm type
					if err := tx.Exec("UPDATE repeaters SET type = 'mmdvm' WHERE type IS NULL OR type = ''").Error; err != nil {
						return fmt.Errorf("could not update existing repeaters: %w", err)
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				if db.Migrator().HasTable(&models.Repeater{}) && db.Migrator().HasColumn(&models.Repeater{}, "type") {
					err := tx.Migrator().DropColumn(&models.Repeater{}, "Type")
					if err != nil {
						return fmt.Errorf("could not drop type column: %w", err)
					}
				}
				return nil
			},
		},
		// Add simplex_repeater column to repeaters for cross-timeslot echo mode
		{
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
		},
	}
}
