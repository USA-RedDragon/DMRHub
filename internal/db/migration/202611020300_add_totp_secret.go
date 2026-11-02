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

// add_totp_secret_migration_202611020300 adds the sealed secret of the
// on-air code of each user. Every user starts with the on-air code off.
func add_totp_secret_migration_202611020300(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type User struct {
		ID         uint   `gorm:"primaryKey"`
		TOTPSecret string `gorm:"size:128;not null;default:''"`
	}

	return &gormigrate.Migration{
		ID: "202611020300",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&User{}) || m.HasColumn(&User{}, "TOTPSecret") {
				return nil
			}
			if err := m.AddColumn(&User{}, "TOTPSecret"); err != nil {
				return fmt.Errorf("could not add totp_secret to users: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasColumn(&User{}, "TOTPSecret") {
				return nil
			}
			if err := m.DropColumn(&User{}, "TOTPSecret"); err != nil {
				return fmt.Errorf("could not drop totp_secret from users: %w", err)
			}
			return nil
		},
	}
}
