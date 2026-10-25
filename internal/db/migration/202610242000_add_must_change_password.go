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
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// add_must_change_password_migration_202610242000 adds the mark of a
// password that an admin set, which the user must replace.
func add_must_change_password_migration_202610242000(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type User struct {
		ID                 uint `gorm:"primaryKey"`
		MustChangePassword bool `gorm:"not null;default:false"`
	}

	return &gormigrate.Migration{
		ID: "202610242000",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&User{}) || m.HasColumn(&User{}, "MustChangePassword") {
				return nil
			}
			if err := m.AddColumn(&User{}, "MustChangePassword"); err != nil {
				return fmt.Errorf("could not add must_change_password to users: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasColumn(&User{}, "MustChangePassword") {
				return nil
			}
			if err := m.DropColumn(&User{}, "MustChangePassword"); err != nil {
				return fmt.Errorf("could not drop must_change_password from users: %w", err)
			}
			return nil
		},
	}
}
