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

func add_positions_migration_202610232100(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type User struct {
		ID             uint `gorm:"primaryKey"`
		SharePositions bool `gorm:"not null;default:false"`
	}
	type PositionSetting struct {
		RadioID         uint `gorm:"primaryKey;autoIncrement:false"`
		UserID          uint `gorm:"not null;index"`
		Tracking        bool `gorm:"not null;default:false"`
		TrackingSince   *time.Time
		IntervalSeconds uint   `gorm:"not null;default:600"`
		RetentionHours  uint   `gorm:"not null;default:24"`
		APRS            bool   `gorm:"not null;default:false"`
		APRSSSID        string `gorm:"size:2"`
		APRSSymbol      string `gorm:"size:16"`
		APRSComment     string `gorm:"size:64"`
		UpdatedAt       time.Time
	}
	type Position struct {
		ID         uint    `gorm:"primarykey"`
		RadioID    uint    `gorm:"not null;index:idx_positions_radio_received,priority:1"`
		Latitude   float64 `gorm:"not null"`
		Longitude  float64 `gorm:"not null"`
		Format     string  `gorm:"size:8;not null"`
		AccuracyM  *float64
		SpeedMPS   *float64
		HeadingDeg *float64
		AltitudeM  *float64
		FixTime    *time.Time
		ReceivedAt time.Time `gorm:"not null;index;index:idx_positions_radio_received,priority:2"`
		RepeaterID uint      `gorm:"not null;default:0"`
		Slot       uint8     `gorm:"not null;default:1"`
		ServiceID  uint      `gorm:"not null;default:0"`
	}
	type LocationRequest struct {
		RadioID         uint   `gorm:"primaryKey;autoIncrement:false"`
		ServiceID       uint   `gorm:"not null"`
		RequestID       []byte `gorm:"not null"`
		IntervalSeconds uint   `gorm:"not null"`
		SentAt          time.Time
		RepeaterID      uint  `gorm:"not null;default:0"`
		Slot            uint8 `gorm:"not null;default:1"`
	}

	return &gormigrate.Migration{
		ID: "202610232100",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable(&User{}) {
				return nil
			}
			if !m.HasColumn(&User{}, "SharePositions") {
				if err := m.AddColumn(&User{}, "SharePositions"); err != nil {
					return fmt.Errorf("could not add share_positions to users: %w", err)
				}
			}
			for _, table := range []any{&PositionSetting{}, &Position{}, &LocationRequest{}} {
				if m.HasTable(table) {
					continue
				}
				if err := m.CreateTable(table); err != nil {
					return fmt.Errorf("could not create a positions table: %w", err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, table := range []any{&LocationRequest{}, &Position{}, &PositionSetting{}} {
				if err := m.DropTable(table); err != nil {
					return fmt.Errorf("could not drop a positions table: %w", err)
				}
			}
			if m.HasColumn(&User{}, "SharePositions") {
				if err := m.DropColumn(&User{}, "SharePositions"); err != nil {
					return fmt.Errorf("could not drop share_positions from users: %w", err)
				}
			}
			return nil
		},
	}
}
