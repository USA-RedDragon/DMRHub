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

import (
	"encoding/json"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// Ratelimit is the model for a rate limit entry
type Ratelimit struct {
	Key       string    `gorm:"primaryKey" json:"key"`
	Hits      int64     `json:"hits"`
	Timestamp time.Time `json:"timestamp"`
}

func (r *Ratelimit) String() string {
	data, err := json.Marshal(r)
	if err != nil {
		slog.Error("Failed to marshal ratelimit to json", "error", err)
		return ""
	}
	return string(data)
}

func FindRatelimitByKey(db *gorm.DB, key string) (*Ratelimit, error) {
	var ratelimit Ratelimit
	if err := db.Where("key = ?", key).First(&ratelimit).Error; err != nil {
		return nil, err
	}
	return &ratelimit, nil
}

func RatelimitKeyExists(db *gorm.DB, key string) (bool, error) {
	var count int64
	if err := db.Model(&Ratelimit{}).
		Where("key = ?", key).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
