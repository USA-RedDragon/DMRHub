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
	"os"
	"path/filepath"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/configulator/v2"
)

func pingConfig(t *testing.T, database config.Database) *config.Config {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	cfg.Database = database
	return &cfg
}

func TestPingReachesANewSQLiteFileAndLeavesNoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "new.db")
	wal := []string{"_pragma=foreign_keys(1)", "_pragma=journal_mode(WAL)", "_pragma=synchronous(NORMAL)"}
	if err := db.Ping(context.Background(), pingConfig(t, config.Database{Driver: config.DatabaseDriverSQLite, Database: path, ExtraParameters: wal})); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Ping left %d files in the directory, the first %s", len(entries), entries[0].Name())
	}
}

func TestPingKeepsAnSQLiteFileThatExists(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	cfg := pingConfig(t, config.Database{Driver: config.DatabaseDriverSQLite, Database: path})
	made, err := db.MakeDB(cfg)
	if err != nil {
		t.Fatalf("MakeDB: %v", err)
	}
	sqlDB, err := made.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(context.Background(), cfg); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	reopened, err := db.MakeDB(cfg)
	if err != nil {
		t.Fatalf("MakeDB after Ping: %v", err)
	}
	if !reopened.Migrator().HasTable("users") {
		t.Fatal("Ping changed the database: the users table is gone")
	}
	if sqlDB, err := reopened.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func TestPingRefusesASQLitePathThatIsADirectory(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "DMRHub.db")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(context.Background(), pingConfig(t, config.Database{Driver: config.DatabaseDriverSQLite, Database: path})); err == nil {
		t.Fatal("Ping reached a SQLite path that is a directory")
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		t.Fatalf("Ping changed the directory: %v", err)
	}
}

func TestPingRefusesAServerThatDoesNotAnswer(t *testing.T) {
	t.Parallel()
	cfg := pingConfig(t, config.Database{
		Driver: config.DatabaseDriverPostgres, Host: "127.0.0.1", Port: 1, Database: "hub", Username: "hub",
		ExtraParameters: []string{"sslmode=disable", "connect_timeout=2"},
	})
	if err := db.Ping(context.Background(), cfg); err == nil {
		t.Fatal("Ping reached a server on a closed port")
	}
}
