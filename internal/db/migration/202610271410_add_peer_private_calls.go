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

// add_peer_private_calls_migration_202610271410 adds the private call
// switches of the peers, off, which keeps what the rules of each peer let
// through.
func add_peer_private_calls_migration_202610271410(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Peer struct {
		ID             uint `gorm:"primaryKey"`
		IngressPrivate bool `gorm:"not null;default:false"`
		EgressPrivate  bool `gorm:"not null;default:false"`
	}

	return &gormigrate.Migration{
		ID: "202610271410",
		Migrate: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable(&Peer{}) {
				return nil
			}
			for _, field := range []string{"IngressPrivate", "EgressPrivate"} {
				if tx.Migrator().HasColumn(&Peer{}, field) {
					continue
				}
				if err := tx.Migrator().AddColumn(&Peer{}, field); err != nil {
					return fmt.Errorf("could not add the %s column to peers: %w", field, err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			for _, field := range []string{"IngressPrivate", "EgressPrivate"} {
				if !tx.Migrator().HasColumn(&Peer{}, field) {
					continue
				}
				if err := tx.Migrator().DropColumn(&Peer{}, field); err != nil {
					return fmt.Errorf("could not drop the %s column from peers: %w", field, err)
				}
			}
			return nil
		},
	}
}
