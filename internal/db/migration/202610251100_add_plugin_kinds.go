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

// add_plugin_kinds_migration_202610251100 adds the plugins that run inside
// DMRHub: the kind of each plugin, what it says it is, the hosts it asks to
// reach and the module of an uploaded one. The plugins from before run as
// their own programs.
func add_plugin_kinds_migration_202610251100(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Plugin struct {
		ID          uint   `gorm:"primaryKey"`
		Kind        string `gorm:"size:16;not null;default:'external'"`
		Builtin     string `gorm:"size:32;not null;default:''"`
		Version     string `gorm:"size:32;not null;default:''"`
		Author      string `gorm:"size:64;not null;default:''"`
		Description string `gorm:"size:500;not null;default:''"`
		ModuleHash  []byte `gorm:"size:32"`
	}
	type PluginHost struct {
		ID       uint   `gorm:"primaryKey"`
		PluginID uint   `gorm:"not null;index"`
		Address  string `gorm:"size:255;not null"`
		Reason   string `gorm:"size:200;not null;default:''"`
		Allowed  bool   `gorm:"not null;default:false"`
	}
	type PluginModule struct {
		PluginID  uint   `gorm:"primaryKey;autoIncrement:false"`
		Hash      []byte `gorm:"size:32;not null"`
		Wasm      []byte `gorm:"not null"`
		CreatedAt time.Time
	}
	columns := []string{"Kind", "Builtin", "Version", "Author", "Description", "ModuleHash"}

	return &gormigrate.Migration{
		ID: "202610251100",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&Plugin{}) {
				return nil
			}
			for _, c := range columns {
				if m.HasColumn(&Plugin{}, c) {
					continue
				}
				if err := m.AddColumn(&Plugin{}, c); err != nil {
					return fmt.Errorf("could not add %s to plugins: %w", c, err)
				}
			}
			for _, table := range []any{&PluginHost{}, &PluginModule{}} {
				if m.HasTable(table) {
					continue
				}
				if err := m.CreateTable(table); err != nil {
					return fmt.Errorf("could not create a plugins table: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, table := range []any{&PluginModule{}, &PluginHost{}} {
				if err := m.DropTable(table); err != nil {
					return fmt.Errorf("could not drop a plugins table: %w", err)
				}
			}
			for _, c := range columns {
				if !m.HasColumn(&Plugin{}, c) {
					continue
				}
				if err := m.DropColumn(&Plugin{}, c); err != nil {
					return fmt.Errorf("could not drop %s from plugins: %w", c, err)
				}
			}
			return nil
		},
	}
}
