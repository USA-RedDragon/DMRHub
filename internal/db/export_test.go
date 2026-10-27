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

package db

import (
	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SetGormLogWriter makes the databases that MakeDB opens next log to w, and
// returns a func that undoes it.
func SetGormLogWriter(w logger.Writer) func() {
	prev := gormLogWriter
	gormLogWriter = w
	return func() { gormLogWriter = prev }
}

// LockMigrations is lockMigrations.
func LockMigrations(db *gorm.DB, config *configPkg.Config) (func() error, error) {
	return lockMigrations(db, config)
}

// Dialect is getDialect.
func Dialect(config *configPkg.Config) gorm.Dialector {
	return getDialect(config)
}

// Migrate is migrate.
func Migrate(db *gorm.DB, config *configPkg.Config) error {
	return migrate(db, config)
}
