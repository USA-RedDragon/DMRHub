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

// add_invites_migration_202610272110 adds the invites that let a person
// register while registration is by invite only.
func add_invites_migration_202610272110(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Invite struct {
		ID        uint      `gorm:"primaryKey"`
		CodeHash  []byte    `gorm:"size:32;not null;uniqueIndex"`
		CreatedBy uint      `gorm:"not null;index"`
		CreatedAt time.Time `gorm:"not null"`
		ExpiresAt time.Time `gorm:"not null;index"`
		UsedBy    *uint
		UsedAt    *time.Time
	}

	return &gormigrate.Migration{
		ID: "202610272110",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasTable(&Invite{}) {
				return nil
			}
			if err := m.CreateTable(&Invite{}); err != nil {
				return fmt.Errorf("could not create the invites table: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable(&Invite{}); err != nil {
				return fmt.Errorf("could not drop the invites table: %w", err)
			}
			return nil
		},
	}
}
