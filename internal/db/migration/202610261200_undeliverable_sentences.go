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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/undeliverable"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// undeliverable_sentences_migration_202610261200 rewrites each entry of
// the old form "<target>:<repeater>:<reason>" in the undeliverable lists
// of the calls as its sentence.
func undeliverable_sentences_migration_202610261200(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Call struct {
		ID            uint     `gorm:"primaryKey"`
		Undeliverable []string `gorm:"serializer:json;type:text"`
	}

	return &gormigrate.Migration{
		ID: "202610261200",
		Migrate: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable(&Call{}) || !tx.Migrator().HasColumn(&Call{}, "Undeliverable") {
				return nil
			}
			var calls []Call
			err := tx.Model(&Call{}).Select("id", "undeliverable").Where("undeliverable LIKE ?", "%:%").FindInBatches(&calls, 500, func(batch *gorm.DB, _ int) error {
				for _, c := range calls {
					changed := false
					for i, entry := range c.Undeliverable {
						if sentence, ok := undeliverable.FromOldForm(entry); ok {
							c.Undeliverable[i] = sentence
							changed = true
						}
					}
					if !changed {
						continue
					}
					if err := batch.Session(&gorm.Session{NewDB: true}).Model(&Call{}).Where("id = ?", c.ID).Select("undeliverable").Updates(&Call{Undeliverable: c.Undeliverable}).Error; err != nil {
						return fmt.Errorf("could not rewrite the undeliverable list of call %d: %w", c.ID, err)
					}
				}
				return nil
			}).Error
			if err != nil {
				return fmt.Errorf("could not rewrite the undeliverable lists: %w", err)
			}
			return nil
		},
		Rollback: func(*gorm.DB) error {
			return nil
		},
	}
}
