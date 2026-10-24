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

// add_codec_columns_migration_202610241000 adds the voice codec of each
// repeater timeslot, the Codec2 setting of OpenBridge peers, and the codec
// and transcoding of each call.
func add_codec_columns_migration_202610241000(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type Repeater struct {
		ID                 uint   `gorm:"primaryKey"`
		TS1Codec           string `gorm:"size:8;not null;default:'auto'"`
		TS2Codec           string `gorm:"size:8;not null;default:'auto'"`
		TS1HeardCodec      string `gorm:"size:8;not null;default:''"`
		TS2HeardCodec      string `gorm:"size:8;not null;default:''"`
		TS1HeardCodec2Mode int    `gorm:"not null;default:0"`
		TS2HeardCodec2Mode int    `gorm:"not null;default:0"`
		TS1HeardAt         *time.Time
		TS2HeardAt         *time.Time
	}
	type Peer struct {
		ID     uint `gorm:"primaryKey"`
		Codec2 bool `gorm:"default:false"`
	}
	type Call struct {
		ID            uint   `gorm:"primarykey"`
		Codec         string `gorm:"size:8"`
		Codec2Mode    int
		SentAMBE      int
		SentCodec2    int
		MissedMS      int
		TranscodeBusy bool
	}
	type columns struct {
		model  any
		table  string
		fields []string
	}
	all := []columns{
		{&Repeater{}, "repeaters", []string{"TS1Codec", "TS2Codec", "TS1HeardCodec", "TS2HeardCodec", "TS1HeardCodec2Mode", "TS2HeardCodec2Mode", "TS1HeardAt", "TS2HeardAt"}},
		{&Peer{}, "peers", []string{"Codec2"}},
		{&Call{}, "calls", []string{"Codec", "Codec2Mode", "SentAMBE", "SentCodec2", "MissedMS", "TranscodeBusy"}},
	}

	return &gormigrate.Migration{
		ID: "202610241000",
		Migrate: func(tx *gorm.DB) error {
			for _, c := range all {
				if !tx.Migrator().HasTable(c.model) {
					continue
				}
				for _, f := range c.fields {
					if tx.Migrator().HasColumn(c.model, f) {
						continue
					}
					if err := tx.Migrator().AddColumn(c.model, f); err != nil {
						return fmt.Errorf("could not add %s to %s: %w", f, c.table, err)
					}
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			for _, c := range all {
				for _, f := range c.fields {
					if !tx.Migrator().HasColumn(c.model, f) {
						continue
					}
					if err := tx.Migrator().DropColumn(c.model, f); err != nil {
						return fmt.Errorf("could not drop %s from %s: %w", f, c.table, err)
					}
				}
			}
			return nil
		},
	}
}
