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

// Package memdb names SQLite memory databases for tests.
//
// An empty SQLite name gives each connection of the pool its own empty
// database, so tables and rows made on one connection are missing on the
// next. A shared-cache name fixes that only while no other open database of
// the process has the same name. The names here are random, so no two tests
// share one, even under -count above one.
package memdb

import (
	"crypto/rand"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

// Name returns a new SQLite name for a shared-cache memory database.
func Name() string {
	return "file:memdb_" + rand.Text()
}

// DSN returns a new SQLite DSN that opens a shared-cache memory database.
func DSN() string {
	return Name() + "?mode=memory&cache=shared"
}

// Use points cfg at a new shared-cache SQLite memory database, with params
// added to the extra parameters.
func Use(cfg *config.Config, params ...string) {
	cfg.Database.Database = Name()
	cfg.Database.ExtraParameters = append([]string{"mode=memory", "cache=shared"}, params...)
}
