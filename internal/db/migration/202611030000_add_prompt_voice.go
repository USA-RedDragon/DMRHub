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

// add_prompt_voice_migration_202611030000 adds the voice of the spoken prompts of
// each user. Every user starts with the default voice of the hub.
func add_prompt_voice_migration_202611030000(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type User struct {
		ID          uint   `gorm:"primaryKey"`
		PromptVoice string `gorm:"size:64;not null;default:''"`
	}

	return &gormigrate.Migration{
		ID: "202611030000",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&User{}) || m.HasColumn(&User{}, "PromptVoice") {
				return nil
			}
			if err := m.AddColumn(&User{}, "PromptVoice"); err != nil {
				return fmt.Errorf("could not add prompt_voice to users: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasColumn(&User{}, "PromptVoice") {
				return nil
			}
			if err := m.DropColumn(&User{}, "PromptVoice"); err != nil {
				return fmt.Errorf("could not drop prompt_voice from users: %w", err)
			}
			return nil
		},
	}
}
