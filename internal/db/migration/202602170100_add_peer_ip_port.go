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

func add_peer_ip_port_migration_202602170100(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Peer struct {
		IP   string `gorm:"column:ip"`
		Port int    `gorm:"column:port"`
	}

	return &gormigrate.Migration{
		ID: "202602170100",
		Migrate: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable("peers") {
				return nil
			}
			if !tx.Migrator().HasColumn(&Peer{}, "ip") {
				if err := tx.Migrator().AddColumn(&Peer{}, "ip"); err != nil {
					return fmt.Errorf("could not add ip column: %w", err)
				}
			}
			if !tx.Migrator().HasColumn(&Peer{}, "port") {
				if err := tx.Migrator().AddColumn(&Peer{}, "port"); err != nil {
					return fmt.Errorf("could not add port column: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if tx.Migrator().HasColumn(&Peer{}, "ip") {
				if err := tx.Migrator().DropColumn(&Peer{}, "ip"); err != nil {
					return fmt.Errorf("could not drop ip column: %w", err)
				}
			}
			if tx.Migrator().HasColumn(&Peer{}, "port") {
				if err := tx.Migrator().DropColumn(&Peer{}, "port"); err != nil {
					return fmt.Errorf("could not drop port column: %w", err)
				}
			}
			return nil
		},
	}
}
