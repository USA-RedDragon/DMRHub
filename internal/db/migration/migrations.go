// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

//nolint:golint,wrapcheck
package migration

import (
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		// convert models.Repeater radio_id to id
		{
			ID: "202302242025",
			Migrate: func(tx *gorm.DB) error {
				if db.Migrator().HasTable(&models.Repeater{}) && db.Migrator().HasColumn(&models.Repeater{}, "radio_id") {
					return tx.Migrator().RenameColumn(&models.Repeater{}, "radio_id", "id")
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				if db.Migrator().HasTable(&models.Repeater{}) && db.Migrator().HasColumn(&models.Repeater{}, "id") && !db.Migrator().HasColumn(&models.Repeater{}, "radio_id") {
					return tx.Migrator().RenameColumn(&models.Repeater{}, "id", "radio_id")
				}
				return nil
			},
		},
	})

	if err := m.Migrate(); err != nil {
		return err
	}

	return nil
}