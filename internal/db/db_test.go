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

package db_test

import (
	"path/filepath"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/configulator"
)

func TestMakeDBInMemoryDatabase(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	db, err := db.MakeDB(&defConfig)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	if db == nil {
		t.Fatal("Expected non-nil database instance, got nil")
	}
}

func TestMakeDBAppSettingsAlreadyExists(t *testing.T) {
	t.Parallel()

	// Use a file-based SQLite DB so we can call MakeDB twice on the same data.
	dbPath := filepath.Join(t.TempDir(), "test.db")

	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	defConfig.Database.Database = dbPath
	defConfig.Database.ExtraParameters = []string{}

	// First call creates the AppSettings record (RowsAffected == 0 path).
	db1, err := db.MakeDB(&defConfig)
	if err != nil {
		t.Fatalf("First MakeDB failed: %v", err)
	}
	if db1 == nil {
		t.Fatal("Expected non-nil database instance from first MakeDB")
	}
	sqlDB1, err := db1.DB()
	if err != nil {
		t.Fatalf("Failed to get sql.DB: %v", err)
	}
	if err := sqlDB1.Close(); err != nil {
		t.Fatalf("Failed to close sql.DB: %v", err)
	}

	// Second call finds the existing AppSettings record.
	// This exercises the result.Error check added as a fix for unchecked
	// db.First error — previously, a non-ErrRecordNotFound error would be
	// silently ignored.
	db2, err := db.MakeDB(&defConfig)
	if err != nil {
		t.Fatalf("Second MakeDB failed: %v", err)
	}
	if db2 == nil {
		t.Fatal("Expected non-nil database instance from second MakeDB")
	}
}
