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

// add_talkgroup_no_listen_migration_202611032200 adds the switch that keeps
// a talkgroup off the listen-only monitor. Every talkgroup starts on it.
func add_talkgroup_no_listen_migration_202611032200(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Talkgroup struct {
		ID       uint `gorm:"primaryKey"`
		NoListen bool `gorm:"not null;default:false"`
	}

	return &gormigrate.Migration{
		ID: "202611032200",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&Talkgroup{}) || m.HasColumn(&Talkgroup{}, "NoListen") {
				return nil
			}
			if err := m.AddColumn(&Talkgroup{}, "NoListen"); err != nil {
				return fmt.Errorf("could not add no_listen to talkgroups: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasColumn(&Talkgroup{}, "NoListen") {
				return nil
			}
			if err := m.DropColumn(&Talkgroup{}, "NoListen"); err != nil {
				return fmt.Errorf("could not drop no_listen from talkgroups: %w", err)
			}
			return nil
		},
	}
}
