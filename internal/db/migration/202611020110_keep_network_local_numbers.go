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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

const localTalkgroupsSetting = "dmr.routing.local-talkgroups"

// keep_network_local_numbers_migration_202611020110 keeps a talkgroup that
// the network had before the local groups came, on the numbers that are
// local by default, 8 and 9, a network talkgroup: it saves the local
// talkgroups in Setup without it. A network whose admins saved the local
// talkgroups already keeps them.
func keep_network_local_numbers_migration_202611020110(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type SetupSettings struct {
		ID        uint `gorm:"primaryKey;autoIncrement:false"`
		Revision  uint64
		Settings  string
		UpdatedAt time.Time
	}
	defaults := []uint{8, 9}

	return &gormigrate.Migration{
		ID: "202611020110",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable("talkgroups") || !m.HasTable("setup_settings") {
				return nil
			}
			var taken []uint
			if err := tx.Table("talkgroups").Where("id IN ? AND deleted_at IS NULL", defaults).Pluck("id", &taken).Error; err != nil {
				return fmt.Errorf("could not read the talkgroups: %w", err)
			}
			if len(taken) == 0 {
				return nil
			}
			keep := []uint{}
			for _, tg := range defaults {
				if !slices.Contains(taken, tg) {
					keep = append(keep, tg)
				}
			}
			var row SetupSettings
			err := tx.Table("setup_settings").Where("id = ?", 1).First(&row).Error
			values := map[string]json.RawMessage{}
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				row = SetupSettings{ID: 1}
			case err != nil:
				return fmt.Errorf("could not read the Setup settings: %w", err)
			default:
				if err := json.Unmarshal([]byte(row.Settings), &values); err != nil {
					return fmt.Errorf("could not decode the Setup settings: %w", err)
				}
				if _, ok := values[localTalkgroupsSetting]; ok {
					return nil
				}
			}
			b, err := json.Marshal(keep)
			if err != nil {
				return fmt.Errorf("could not encode the local talkgroups: %w", err)
			}
			values[localTalkgroupsSetting] = b
			s, err := json.Marshal(values)
			if err != nil {
				return fmt.Errorf("could not encode the Setup settings: %w", err)
			}
			row.Settings, row.Revision, row.UpdatedAt = string(s), row.Revision+1, time.Now()
			if err := tx.Table("setup_settings").Save(&row).Error; err != nil {
				return fmt.Errorf("could not save the Setup settings: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable("setup_settings") {
				return nil
			}
			var row SetupSettings
			err := tx.Table("setup_settings").Where("id = ?", 1).First(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("could not read the Setup settings: %w", err)
			}
			values := map[string]json.RawMessage{}
			if err := json.Unmarshal([]byte(row.Settings), &values); err != nil {
				return fmt.Errorf("could not decode the Setup settings: %w", err)
			}
			if _, ok := values[localTalkgroupsSetting]; !ok {
				return nil
			}
			delete(values, localTalkgroupsSetting)
			s, err := json.Marshal(values)
			if err != nil {
				return fmt.Errorf("could not encode the Setup settings: %w", err)
			}
			row.Settings, row.Revision, row.UpdatedAt = string(s), row.Revision+1, time.Now()
			if err := tx.Table("setup_settings").Save(&row).Error; err != nil {
				return fmt.Errorf("could not save the Setup settings: %w", err)
			}
			return nil
		},
	}
}
