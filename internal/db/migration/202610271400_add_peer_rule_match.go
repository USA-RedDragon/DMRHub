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

// add_peer_rule_match_migration_202610271400 adds the match of the peer
// rules and gives the rules made before it the IDs they checked: the
// destination for an ingress rule, the source for an egress rule.
func add_peer_rule_match_migration_202610271400(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type PeerRule struct {
		ID        uint `gorm:"primaryKey"`
		Direction bool
		Match     string `gorm:"column:match_kind;size:16;not null;default:''"`
	}

	return &gormigrate.Migration{
		ID: "202610271400",
		Migrate: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable(&PeerRule{}) {
				return nil
			}
			if !tx.Migrator().HasColumn(&PeerRule{}, "Match") {
				if err := tx.Migrator().AddColumn(&PeerRule{}, "Match"); err != nil {
					return fmt.Errorf("could not add the match column to peer rules: %w", err)
				}
			}
			for _, set := range []struct {
				ingress bool
				match   string
			}{{true, "destination"}, {false, "source"}} {
				if err := tx.Model(&PeerRule{}).Where("direction = ? AND match_kind = ?", set.ingress, "").Update("match_kind", set.match).Error; err != nil {
					return fmt.Errorf("could not set the match of the peer rules: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if tx.Migrator().HasColumn(&PeerRule{}, "Match") {
				if err := tx.Migrator().DropColumn(&PeerRule{}, "Match"); err != nil {
					return fmt.Errorf("could not drop the match column from peer rules: %w", err)
				}
			}
			return nil
		},
	}
}
