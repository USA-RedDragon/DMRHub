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

// add_generated_secrets_migration_202610261600 adds the table of the
// secrets that DMRHub makes when no deployment sets them.
func add_generated_secrets_migration_202610261600(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type GeneratedSecret struct {
		Name  string `gorm:"primaryKey;size:64"`
		Value string `gorm:"type:text;not null"`
	}

	return &gormigrate.Migration{
		ID: "202610261600",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasTable(&GeneratedSecret{}) {
				return nil
			}
			if err := m.CreateTable(&GeneratedSecret{}); err != nil {
				return fmt.Errorf("could not create the generated_secrets table: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable(&GeneratedSecret{}); err != nil {
				return fmt.Errorf("could not drop the generated_secrets table: %w", err)
			}
			return nil
		},
	}
}
