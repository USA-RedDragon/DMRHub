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

func add_offline_alerts_migration_202611011703(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type OfflineAlert struct {
		UserID       uint      `gorm:"primaryKey;autoIncrement:false"`
		RepeaterID   uint      `gorm:"primaryKey;autoIncrement:false;index"`
		DelayMinutes uint      `gorm:"not null;default:10"`
		Back         bool      `gorm:"not null;default:false"`
		CreatedAt    time.Time `gorm:"not null"`
		SentAt       *time.Time
		Since        *time.Time
	}

	return &gormigrate.Migration{
		ID: "202611011703",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasTable("offline_alerts") {
				return nil
			}
			if err := m.CreateTable(&OfflineAlert{}); err != nil {
				return fmt.Errorf("could not create the offline_alerts table: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable("offline_alerts"); err != nil {
				return fmt.Errorf("could not drop the offline_alerts table: %w", err)
			}
			return nil
		},
	}
}
