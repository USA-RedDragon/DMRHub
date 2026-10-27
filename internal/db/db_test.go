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
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"gorm.io/gorm"
)

func TestMakeDBInMemoryDatabase(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	memdb.Use(&defConfig)
	db, err := db.MakeDB(&defConfig)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	if db == nil {
		t.Fatal("Expected non-nil database instance, got nil")
	}
}

func TestMakeDBTwiceOnTheSameData(t *testing.T) {
	t.Parallel()

	// Use a file-based SQLite DB so we can call MakeDB twice on the same data.
	dbPath := filepath.Join(t.TempDir(), "test.db")

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	defConfig.Database.Database = dbPath
	defConfig.Database.ExtraParameters = []string{}

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

	db2, err := db.MakeDB(&defConfig)
	if err != nil {
		t.Fatalf("Second MakeDB failed: %v", err)
	}
	if db2 == nil {
		t.Fatal("Expected non-nil database instance from second MakeDB")
	}
	if db2.Migrator().HasTable("app_settings") {
		t.Error("MakeDB made the app_settings table")
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

// TestSQLiteSynchronousIsNormal checks that each connection to a SQLite
// file commits with synchronous=NORMAL, so that a commit in WAL mode does
// not wait for the disk, also when the extra parameters of an install leave
// it out, and that a synchronous pragma of the install wins.
func TestSQLiteSynchronousIsNormal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		params []string
		want   int
	}{
		{name: "default", want: 1},
		{name: "own parameters", params: []string{"_pragma=foreign_keys(1)", "_pragma=journal_mode(WAL)"}, want: 1},
		{name: "own synchronous", params: []string{"_pragma=journal_mode(WAL)", "_pragma=synchronous(FULL)"}, want: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			if err != nil {
				t.Fatalf("Failed to create default config: %v", err)
			}
			cfg.Database.Database = filepath.Join(t.TempDir(), "sync.db")
			if tc.params != nil {
				cfg.Database.ExtraParameters = tc.params
			}
			gdb, err := db.MakeDB(&cfg)
			if err != nil {
				t.Fatalf("Failed to create database: %v", err)
			}
			sqlDB, err := gdb.DB()
			if err != nil {
				t.Fatalf("Failed to get the pool: %v", err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			conns := make([]interface{ Close() error }, 0, 4)
			for range 4 {
				c, err := sqlDB.Conn(context.Background())
				if err != nil {
					t.Fatalf("Failed to take a connection: %v", err)
				}
				conns = append(conns, c)
				var got int
				if err := c.QueryRowContext(context.Background(), "PRAGMA synchronous").Scan(&got); err != nil {
					t.Fatalf("Failed to read synchronous: %v", err)
				}
				if got != tc.want {
					t.Errorf("connection %d: synchronous is %d, want %d", len(conns), got, tc.want)
				}
			}
			for _, c := range conns {
				_ = c.Close()
			}
		})
	}
}

// TestSQLiteWritersTakeTurns writes once while another writer writes
// again as soon as it commits, each write taking 20 ms. The lone write
// must get its turn before its busy timeout of 500 ms runs out.
func TestSQLiteWritersTakeTurns(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	defConfig.Database.Database = filepath.Join(t.TempDir(), "turns.db")
	defConfig.Database.ExtraParameters = append(defConfig.Database.ExtraParameters, "_pragma=busy_timeout(500)")
	gdb, err := db.MakeDB(&defConfig)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("Failed to get the pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	p, err := models.EnsureBuiltinPlugin(gdb, "parrot", "Parrot", "v1", []models.PluginRange{{First: 9990, Last: 9990}}, time.Now())
	if err != nil {
		t.Fatalf("Failed to add the plugin: %v", err)
	}
	if err := models.NotePluginConnected(gdb, p.ID, "s", "", time.Now()); err != nil {
		t.Fatalf("Failed to connect the plugin: %v", err)
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	busy := make(chan struct{})
	wg.Go(func() {
		for i := 0; !stop.Load(); i++ {
			err := gdb.Transaction(func(tx *gorm.DB) error {
				if err := models.NotePluginSeen(tx, p.ID, "s", time.Now()); err != nil {
					return fmt.Errorf("busy write: %w", err)
				}
				if i == 0 {
					close(busy)
				}
				time.Sleep(20 * time.Millisecond)
				return nil
			})
			if err != nil {
				t.Errorf("the busy writer failed: %v", err)
				return
			}
		}
	})
	<-busy
	err = models.NotePluginSeen(gdb, p.ID, "s", time.Now())
	stop.Store(true)
	wg.Wait()
	if err != nil {
		t.Fatalf("the lone writer got no turn: %v", err)
	}
}
