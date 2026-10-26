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

package liveconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// saveAttempts is how many times a save tries again when another save
// changes the settings between its read and its write.
const saveAttempts = 10

// Saved is the settings that admins saved in Setup, by path, with the
// revision of the last save.
type Saved struct {
	Revision uint64
	Values   map[string]json.RawMessage
}

func decodeRow(row models.SetupSettings) (Saved, error) {
	s := Saved{Revision: row.Revision, Values: map[string]json.RawMessage{}}
	if row.Settings == "" {
		return s, nil
	}
	if err := json.Unmarshal([]byte(row.Settings), &s.Values); err != nil {
		return s, fmt.Errorf("the saved Setup settings are not JSON: %w", err)
	}
	return s, nil
}

// readRow returns the row of the saved settings, and whether there is one.
func readRow(ctx context.Context, db *gorm.DB) (models.SetupSettings, bool, error) {
	var rows []models.SetupSettings
	if err := db.WithContext(ctx).Where("id = ?", models.SetupSettingsID).Limit(1).Find(&rows).Error; err != nil {
		return models.SetupSettings{}, false, fmt.Errorf("failed to read the saved Setup settings: %w", err)
	}
	if len(rows) == 0 {
		return models.SetupSettings{}, false, nil
	}
	return rows[0], true, nil
}

// LoadSaved returns the settings that admins saved in Setup. It returns
// no settings at revision 0 when none were ever saved.
func LoadSaved(ctx context.Context, db *gorm.DB) (Saved, error) {
	row, ok, err := readRow(ctx, db)
	if err != nil {
		return Saved{}, err
	}
	if !ok {
		return Saved{Values: map[string]json.RawMessage{}}, nil
	}
	return decodeRow(row)
}

// createRow makes the row of the saved settings with values, unless a
// replica made it first. It reports whether it made the row.
func createRow(ctx context.Context, db *gorm.DB, values map[string]json.RawMessage) (bool, error) {
	b, err := json.Marshal(values)
	if err != nil {
		return false, fmt.Errorf("failed to encode the Setup settings: %w", err)
	}
	res := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&models.SetupSettings{ID: models.SetupSettingsID, Settings: string(b), UpdatedAt: time.Now()})
	if res.Error != nil {
		return false, fmt.Errorf("failed to create the Setup settings: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// store changes the saved settings with change and returns the new
// revision. A save of another replica between the read and the write
// makes it read again and change the new settings.
func store(ctx context.Context, db *gorm.DB, change func(map[string]json.RawMessage)) (uint64, error) {
	for range saveAttempts {
		row, ok, err := readRow(ctx, db)
		if err != nil {
			return 0, err
		}
		if !ok {
			if _, err := createRow(ctx, db, map[string]json.RawMessage{}); err != nil {
				return 0, err
			}
			continue
		}
		saved, err := decodeRow(row)
		if err != nil {
			return 0, err
		}
		change(saved.Values)
		b, err := json.Marshal(saved.Values)
		if err != nil {
			return 0, fmt.Errorf("failed to encode the Setup settings: %w", err)
		}
		res := db.WithContext(ctx).Model(&models.SetupSettings{}).
			Where("id = ? AND revision = ?", models.SetupSettingsID, row.Revision).
			Updates(map[string]any{"settings": string(b), "revision": row.Revision + 1, "updated_at": time.Now()})
		if res.Error != nil {
			return 0, fmt.Errorf("failed to save the Setup settings: %w", res.Error)
		}
		if res.RowsAffected == 1 {
			return row.Revision + 1, nil
		}
	}
	return 0, ErrBusy
}

// ErrBusy is a save that other saves kept from its write.
var ErrBusy = errors.New("other saves of the Setup settings kept this one from its write; try again")
