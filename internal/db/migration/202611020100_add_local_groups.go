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

// add_local_groups_migration_202611020100 adds the local groups, their
// repeaters, and the local group of each call.
func add_local_groups_migration_202611020100(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type LocalGroup struct {
		ID        uint   `gorm:"primarykey"`
		Name      string `gorm:"size:64;not null"`
		Talkgroup uint   `gorm:"not null"`
		OwnerID   uint   `gorm:"not null;index"`
		CreatedAt time.Time
		UpdatedAt time.Time
	}
	type LocalGroupMember struct {
		LocalGroupID uint   `gorm:"primaryKey;autoIncrement:false"`
		RepeaterID   uint   `gorm:"primaryKey;autoIncrement:false;uniqueIndex:idx_local_group_members_number,priority:1"`
		Talkgroup    uint   `gorm:"not null;uniqueIndex:idx_local_group_members_number,priority:2"`
		Slot         uint8  `gorm:"not null"`
		State        string `gorm:"size:8;not null"`
		AskedByID    uint   `gorm:"not null;default:0"`
		CreatedAt    time.Time
	}
	type Call struct {
		ID             uint `gorm:"primarykey"`
		IsToLocalGroup bool `gorm:"not null;default:false"`
		LocalGroupID   *uint
		LocalGroupName string `gorm:"size:64"`
	}
	columns := []string{"IsToLocalGroup", "LocalGroupID", "LocalGroupName"}

	return &gormigrate.Migration{
		ID: "202611020100",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, t := range []any{&LocalGroup{}, &LocalGroupMember{}} {
				if m.HasTable(t) {
					continue
				}
				if err := m.CreateTable(t); err != nil {
					return fmt.Errorf("could not create the %T table: %w", t, err)
				}
			}
			if !m.HasTable(&Call{}) {
				return nil
			}
			for _, col := range columns {
				if m.HasColumn(&Call{}, col) {
					continue
				}
				if err := m.AddColumn(&Call{}, col); err != nil {
					return fmt.Errorf("could not add %s column to calls: %w", col, err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range columns {
				if !m.HasColumn(&Call{}, col) {
					continue
				}
				if err := m.DropColumn(&Call{}, col); err != nil {
					return fmt.Errorf("could not drop %s column from calls: %w", col, err)
				}
			}
			if err := m.DropTable("local_group_members", "local_groups"); err != nil {
				return fmt.Errorf("could not drop the local group tables: %w", err)
			}
			return nil
		},
	}
}
