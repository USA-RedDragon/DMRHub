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
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/configulator/v2"
)

func TestMakeDBInMemoryDatabase(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
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

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
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

func TestMakeDBKeepsConnectionsOpenAfterABusySpell(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	defConfig.Database.Database = filepath.Join(t.TempDir(), "busy.db")
	gdb, err := db.MakeDB(&defConfig)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("Failed to get the pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	busy := sqlDB.Stats().MaxOpenConnections
	conns := make([]interface{ Close() error }, 0, busy)
	for range busy {
		c, err := sqlDB.Conn(context.Background())
		if err != nil {
			t.Fatalf("Failed to take a connection: %v", err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		_ = c.Close()
	}
	if closed := sqlDB.Stats().MaxIdleClosed; closed != 0 {
		t.Fatalf("%d of %d connections were closed when they went back to the pool", closed, busy)
	}
}

func TestSQLiteTransactionsThatReadThenWriteRunTogether(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	defConfig.Database.Database = filepath.Join(t.TempDir(), "readwrite.db")
	gdb, err := db.MakeDB(&defConfig)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("Failed to get the pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	ranges := []models.PluginRange{{First: 9990, Last: 9990}}
	p, err := models.EnsureBuiltinPlugin(gdb, "parrot", "Parrot", "v1", ranges, time.Now())
	if err != nil {
		t.Fatalf("Failed to add the plugin: %v", err)
	}
	if err := models.NotePluginConnected(gdb, p.ID, "s", "", time.Now()); err != nil {
		t.Fatalf("Failed to connect the plugin: %v", err)
	}

	const workers, rounds = 4, 25
	errs := make(chan error, (workers+1)*rounds)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for range rounds {
				if _, err := models.EnsureBuiltinPlugin(gdb, "parrot", "Parrot", fmt.Sprintf("v%d", w), ranges, time.Now()); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Go(func() {
		for range rounds {
			if err := models.NotePluginSeen(gdb, p.ID, "s", time.Now()); err != nil {
				errs <- err
			}
		}
	})
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
