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

func add_nets_tables_migration_202602160300(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type ScheduledNet struct {
		ID              uint `gorm:"primarykey"`
		TalkgroupID     uint `gorm:"not null;index"`
		CreatedByUserID uint
		Name            string
		Description     string
		CronExpression  string
		DayOfWeek       int
		TimeOfDay       string
		Timezone        string
		DurationMinutes *uint
		Enabled         bool `gorm:"default:true"`
		NextRun         *time.Time
		CreatedAt       time.Time
		UpdatedAt       time.Time
		DeletedAt       gorm.DeletedAt `gorm:"index"`
	}

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
		CreatedAt       time.Time
		UpdatedAt       time.Time
		DeletedAt       gorm.DeletedAt `gorm:"index"`
	}

	return &gormigrate.Migration{
		ID: "202602160300",
		Migrate: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable(&ScheduledNet{}) {
				if err := tx.AutoMigrate(&ScheduledNet{}); err != nil {
					return fmt.Errorf("could not create scheduled_nets table: %w", err)
				}
			}
			if !tx.Migrator().HasTable(&Net{}) {
				if err := tx.AutoMigrate(&Net{}); err != nil {
					return fmt.Errorf("could not create nets table: %w", err)
				}
			}
			// Add a unique index to enforce at most one active net per talkgroup.
			// For Postgres this uses a partial index; for SQLite we create a
			// unique index on (talkgroup_id, active) which combined with the
			// application-level check achieves the same goal.
			if err := tx.Exec(
				"CREATE UNIQUE INDEX IF NOT EXISTS idx_nets_one_active_per_tg ON nets (talkgroup_id) WHERE active = true",
			).Error; err != nil {
				// SQLite does not support WHERE on CREATE INDEX in all versions.
				// Fall back to a composite unique index.
				if err2 := tx.Exec(
					"CREATE UNIQUE INDEX IF NOT EXISTS idx_nets_one_active_per_tg ON nets (talkgroup_id, active) WHERE active = 1",
				).Error; err2 != nil {
					// If both fail, log but don't block migration — the application
					// layer enforces the constraint as well.
					fmt.Printf("warning: could not create partial unique index for active nets: %v / %v\n", err, err2)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if tx.Migrator().HasTable(&Net{}) {
				if err := tx.Migrator().DropTable(&Net{}); err != nil {
					return fmt.Errorf("could not drop nets table: %w", err)
				}
			}
			if tx.Migrator().HasTable(&ScheduledNet{}) {
				if err := tx.Migrator().DropTable(&ScheduledNet{}); err != nil {
					return fmt.Errorf("could not drop scheduled_nets table: %w", err)
				}
			}
			return nil
		},
	}
}
