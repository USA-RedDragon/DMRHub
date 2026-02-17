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
	"log/slog"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func fix_ipsc_repeater_data_migration_202602160200(db *gorm.DB, cfg *config.Config) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202602160200",
		Migrate: func(tx *gorm.DB) error {
			if !db.Migrator().HasTable(&models.Repeater{}) {
				return nil
			}

			// Skip data population if radio ID validation is disabled
			if cfg.DMR.DisableRadioIDValidation {
				return nil
			}

			// Fetch all repeaters with 6-digit IDs (real repeaters, not hotspots)
			var repeaters []models.Repeater
			if err := tx.Where("id >= 100000 AND id <= 999999").Find(&repeaters).Error; err != nil {
				return fmt.Errorf("could not fetch repeaters: %w", err)
			}

			for i := range repeaters {
				r, ok := repeaterdb.Get(repeaters[i].ID)
				if !ok {
					slog.Warn("Repeater not found in repeaterdb during migration, skipping",
						"repeater_id", repeaters[i].ID)
					continue
				}

				repeaters[i].Callsign = r.Callsign
				if r.ColorCode > 15 {
					slog.Error("Color code out of range for uint8", "colorCode", r.ColorCode)
					continue
				}
				repeaters[i].ColorCode = uint8(r.ColorCode)
				repeaters[i].City = r.City
				repeaters[i].State = r.State
				repeaters[i].Country = r.Country
				repeaters[i].Description = r.MapInfo
				// r.Frequency is a string in MHz with a decimal, convert to an int in Hz and set repeater.RXFrequency
				mhZFloat, parseErr := strconv.ParseFloat(r.Frequency, 32)
				if parseErr != nil {
					slog.Error("Error converting frequency to float", "error", parseErr)
					continue
				}
				const mHzToHz = 1000000
				repeaters[i].TXFrequency = uint(mhZFloat * mHzToHz)
				// r.Offset is a string with +/- and a decimal in MHz, convert to an int in Hz and set repeater.TXFrequency to RXFrequency +/- Offset
				var positiveOffset bool
				if strings.HasPrefix(r.Offset, "-") {
					positiveOffset = false
				} else {
					positiveOffset = true
				}
				// strip the +/- from the offset
				r.Offset = strings.TrimPrefix(r.Offset, "-")
				r.Offset = strings.TrimPrefix(r.Offset, "+")
				// convert the offset to a float
				offsetFloat, parseErr := strconv.ParseFloat(r.Offset, 32)
				if parseErr != nil {
					slog.Error("Error converting offset to float", "offset", r.Offset, "error", parseErr)
					continue
				}
				// convert the offset to an int in Hz
				offsetInt := uint(offsetFloat * mHzToHz)
				if positiveOffset {
					repeaters[i].RXFrequency = repeaters[i].TXFrequency + offsetInt
				} else {
					repeaters[i].RXFrequency = repeaters[i].TXFrequency - offsetInt
				}

				if err := tx.Save(&repeaters[i]).Error; err != nil {
					return fmt.Errorf("could not update repeater %d: %w", repeaters[i].ID, err)
				}
			}

			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if !db.Migrator().HasTable(&models.Repeater{}) {
				return nil
			}
			return nil
		},
	}
}
