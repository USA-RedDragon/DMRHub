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

func add_plugins_migration_202610240100(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Plugin struct {
		ID              uint      `gorm:"primaryKey"`
		Name            string    `gorm:"size:64;not null;uniqueIndex"`
		TokenHash       []byte    `gorm:"size:32;not null;uniqueIndex"`
		TokenCreatedAt  time.Time `gorm:"not null"`
		Enabled         bool      `gorm:"not null;default:true"`
		LastConnectedAt *time.Time
		LastSeenAt      *time.Time
		LastAddress     string `gorm:"size:64"`
		Session         string `gorm:"size:32;not null;default:''"`
		CreatedAt       time.Time
		UpdatedAt       time.Time
	}
	type PluginRange struct {
		ID       uint `gorm:"primaryKey"`
		PluginID uint `gorm:"not null;index"`
		First    uint `gorm:"column:first_id;not null"`
		Last     uint `gorm:"column:last_id;not null"`
	}

	return &gormigrate.Migration{
		ID: "202610240100",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, table := range []any{&Plugin{}, &PluginRange{}} {
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
			for _, table := range []any{&PluginRange{}, &Plugin{}} {
				if err := m.DropTable(table); err != nil {
					return fmt.Errorf("could not drop a plugins table: %w", err)
				}
			}
			return nil
		},
	}
}
