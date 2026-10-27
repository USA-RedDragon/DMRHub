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

// builtin_plugin_names_migration_202610261300 drops the unique index of the
// plugin names, so that a built-in plugin and another plugin can have the
// same name. The automatic migration then adds the unique index of the
// name with the built-in key.
func builtin_plugin_names_migration_202610261300(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Plugin struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:64;not null;uniqueIndex"`
	}
	const index = "idx_plugins_name"

	return &gormigrate.Migration{
		ID: "202610261300",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&Plugin{}) || !m.HasIndex(&Plugin{}, index) {
				return nil
			}
			if err := m.DropIndex(&Plugin{}, index); err != nil {
				return fmt.Errorf("could not drop the unique index of the plugin names: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&Plugin{}) || m.HasIndex(&Plugin{}, index) {
				return nil
			}
			if err := m.CreateIndex(&Plugin{}, index); err != nil {
				return fmt.Errorf("could not add the unique index of the plugin names: %w", err)
			}
			return nil
		},
	}
}
