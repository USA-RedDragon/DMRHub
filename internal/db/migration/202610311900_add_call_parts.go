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

// add_call_parts_migration_202610311900 adds the parts of the transmissions
// that moved between replicas, by the transmission they were merged into.
func add_call_parts_migration_202610311900(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type CallPart struct {
		CallID uint   `gorm:"primaryKey;autoIncrement:false"`
		PartID string `gorm:"primaryKey;size:64"`
	}

	return &gormigrate.Migration{
		ID: "202610311900",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable("call_parts") {
				if err := m.CreateTable(&CallPart{}); err != nil {
					return fmt.Errorf("could not create the call_parts table: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable("call_parts"); err != nil {
				return fmt.Errorf("could not drop the call_parts table: %w", err)
			}
			return nil
		},
	}
}
