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
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// IDs returns the IDs of every migration that Migrate and MigrateSchema
// run, which share one table of the migrations done.
func IDs() []string {
	cfg := &config.Config{}
	all := append(migrations(nil, cfg), schemaMigrations(nil, cfg)...)
	ids := make([]string, 0, len(all))
	for _, m := range all {
		ids = append(ids, m.ID)
	}
	return ids
}

// Rollback rolls back the migration id of Migrate.
func Rollback(db *gorm.DB, cfg *config.Config, id string) error {
	all := migrations(db, cfg)
	for _, m := range all {
		if m.ID == id {
			return gormigrate.New(db, gormigrate.DefaultOptions, all).RollbackMigration(m) //nolint:wrapcheck
		}
	}
	return gorm.ErrRecordNotFound
}

// ByID returns the migration id of Migrate, or nil.
func ByID(id string) *gormigrate.Migration {
	for _, m := range migrations(nil, &config.Config{}) {
		if m.ID == id {
			return m
		}
	}
	return nil
}
