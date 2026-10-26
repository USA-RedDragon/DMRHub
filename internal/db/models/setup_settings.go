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

package models

import "time"

// SetupSettingsID is the ID of the one row of SetupSettings.
const SetupSettingsID = 1

// SetupSettings holds the settings that admins save in Setup, which every
// replica runs with where its config file, environment and flags leave
// them unset. Settings is a JSON object of the values, by the dotted path
// of the setting, such as dmr.ipsc.network-id. Revision counts the saves.
// It never holds a secret or a setting that takes effect at start only.
type SetupSettings struct {
	ID        uint   `gorm:"primaryKey;autoIncrement:false"`
	Revision  uint64 `gorm:"not null;default:0"`
	Settings  string `gorm:"type:text;not null"`
	UpdatedAt time.Time
}

func (SetupSettings) TableName() string {
	return "setup_settings"
}
