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
	"database/sql/driver"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Blob is binary data of any length. Postgres and SQLite keep it in a
// bytea column, and MySQL and MariaDB in a longblob column.
type Blob []byte

// GormDBDataType is the column type of a Blob on the database of db.
func (Blob) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Name() == "mysql" {
		return "longblob"
	}
	return "bytea"
}

// Value is the Blob as the bytes it holds.
func (b Blob) Value() (driver.Value, error) {
	return []byte(b), nil
}
