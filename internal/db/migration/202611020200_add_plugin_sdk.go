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

// add_plugin_sdk_migration_202611020200 adds the store of each plugin, the
// switches that plugins declare and how each user set them.
func add_plugin_sdk_migration_202611020200(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type PluginStoreEntry struct {
		PluginID  uint       `gorm:"primaryKey;autoIncrement:false"`
		KeyHex    string     `gorm:"primaryKey;size:510"`
		Value     []byte     `gorm:"not null"`
		ExpiresAt *time.Time `gorm:"index"`
		UpdatedAt time.Time
	}
	type PluginSwitch struct {
		PluginID uint   `gorm:"primaryKey;autoIncrement:false"`
		Key      string `gorm:"primaryKey;size:32"`
		Position int    `gorm:"not null;default:0"`
		Spec     string `gorm:"type:text;not null"`
	}
	type PluginUserSwitch struct {
		PluginID  uint   `gorm:"primaryKey;autoIncrement:false"`
		UserID    uint   `gorm:"primaryKey;autoIncrement:false;index"`
		Key       string `gorm:"primaryKey;size:32"`
		On        bool   `gorm:"column:is_on;not null;default:false"`
		Choice    string `gorm:"size:16;not null;default:''"`
		UpdatedAt time.Time
	}
	tables := []struct {
		name  string
		model any
	}{{"plugin_store_entries", &PluginStoreEntry{}}, {"plugin_switches", &PluginSwitch{}}, {"plugin_user_switches", &PluginUserSwitch{}}}

	return &gormigrate.Migration{
		ID: "202611020200",
		Migrate: func(tx *gorm.DB) error {
			for _, t := range tables {
				if tx.Migrator().HasTable(t.name) {
					continue
				}
				if err := tx.Migrator().CreateTable(t.model); err != nil {
					return fmt.Errorf("could not create the %s table: %w", t.name, err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			for _, t := range tables {
				if err := tx.Migrator().DropTable(t.name); err != nil {
					return fmt.Errorf("could not drop the %s table: %w", t.name, err)
				}
			}
			return nil
		},
	}
}
