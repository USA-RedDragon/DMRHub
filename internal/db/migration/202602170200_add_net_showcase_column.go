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

func add_net_showcase_column_migration_202602170200(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Net struct {
		ID              uint `gorm:"primarykey"`
		TalkgroupID     uint `gorm:"not null;index"`
		StartedByUserID uint
		ScheduledNetID  *uint
		StartTime       time.Time
		EndTime         *time.Time
		DurationMinutes *uint
		Description     string
		Active          bool `gorm:"index"`
		Showcase        bool `gorm:"default:false"`
		CreatedAt       time.Time
		UpdatedAt       time.Time
		DeletedAt       gorm.DeletedAt `gorm:"index"`
	}

	return &gormigrate.Migration{
		ID: "202602170200",
		Migrate: func(tx *gorm.DB) error {
			if !tx.Migrator().HasColumn(&Net{}, "Showcase") {
				if err := tx.Migrator().AddColumn(&Net{}, "Showcase"); err != nil {
					return fmt.Errorf("could not add showcase column to nets: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if tx.Migrator().HasColumn(&Net{}, "Showcase") {
				if err := tx.Migrator().DropColumn(&Net{}, "Showcase"); err != nil {
					return fmt.Errorf("could not drop showcase column from nets: %w", err)
				}
			}
			return nil
		},
	}
}
