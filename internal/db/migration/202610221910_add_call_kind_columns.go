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

func add_call_kind_columns_migration_202610221910(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Call struct {
		ID            uint `gorm:"primarykey"`
		UserID        *uint
		RepeaterID    *uint
		Kind          string `gorm:"size:16;not null;default:'voice';index"`
		SubKind       string `gorm:"size:64"`
		SourceID      uint   `gorm:"index"`
		Transport     string `gorm:"size:16"`
		IngressID     uint
		Bursts        uint
		FID           *uint8
		Opcode        *uint8
		SAP           *uint8
		DataFormat    string `gorm:"size:16"`
		Confirmed     bool
		Blocks        uint
		Octets        uint
		BadCRC        uint
		LostBlocks    uint
		Response      string `gorm:"size:8"`
		TalkerAlias   string `gorm:"size:64"`
		AliasFormat   string `gorm:"size:16"`
		Latitude      *float64
		Longitude     *float64
		PositionError *uint8
		Partial       bool
		Undeliverable []string `gorm:"serializer:json;type:text"`
	}
	columns := []string{
		"Kind", "SubKind", "SourceID", "Transport", "IngressID", "Bursts", "FID", "Opcode", "SAP",
		"DataFormat", "Confirmed", "Blocks", "Octets", "BadCRC", "LostBlocks", "Response",
		"TalkerAlias", "AliasFormat", "Latitude", "Longitude", "PositionError", "Partial", "Undeliverable",
	}

	return &gormigrate.Migration{
		ID: "202610221910",
		Migrate: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable(&Call{}) {
				return nil
			}
			for _, col := range columns {
				if tx.Migrator().HasColumn(&Call{}, col) {
					continue
				}
				if err := tx.Migrator().AddColumn(&Call{}, col); err != nil {
					return fmt.Errorf("could not add %s column to calls: %w", col, err)
				}
			}
			if err := tx.Exec("UPDATE calls SET kind = 'voice' WHERE kind IS NULL OR kind = ''").Error; err != nil {
				return fmt.Errorf("could not set the kind of calls: %w", err)
			}
			if err := tx.Exec("UPDATE calls SET source_id = user_id WHERE (source_id IS NULL OR source_id = 0) AND user_id IS NOT NULL").Error; err != nil {
				return fmt.Errorf("could not set the source of calls: %w", err)
			}
			if err := tx.Exec("UPDATE calls SET ingress_id = repeater_id WHERE (ingress_id IS NULL OR ingress_id = 0) AND repeater_id IS NOT NULL").Error; err != nil {
				return fmt.Errorf("could not set the ingress of calls: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			for _, col := range columns {
				if !tx.Migrator().HasColumn(&Call{}, col) {
					continue
				}
				if err := tx.Migrator().DropColumn(&Call{}, col); err != nil {
					return fmt.Errorf("could not drop %s column from calls: %w", col, err)
				}
			}
			return nil
		},
	}
}
