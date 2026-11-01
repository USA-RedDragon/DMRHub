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

// add_message_rate_migration_202611011800 adds the rate of the blocks of
// each data message and whether a P_HEAD came with it.
func add_message_rate_migration_202611011800(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Message struct {
		ID          uint  `gorm:"primarykey"`
		Rate        uint8 `gorm:"not null;default:0"`
		Proprietary bool  `gorm:"not null;default:false"`
	}
	fields := []string{"Rate", "Proprietary"}

	return &gormigrate.Migration{
		ID: "202611011800",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&Message{}) {
				return nil
			}
			for _, f := range fields {
				if m.HasColumn(&Message{}, f) {
					continue
				}
				if err := m.AddColumn(&Message{}, f); err != nil {
					return fmt.Errorf("could not add %s to messages: %w", f, err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, f := range fields {
				if !m.HasColumn(&Message{}, f) {
					continue
				}
				if err := m.DropColumn(&Message{}, f); err != nil {
					return fmt.Errorf("could not drop %s from messages: %w", f, err)
				}
			}
			return nil
		},
	}
}
