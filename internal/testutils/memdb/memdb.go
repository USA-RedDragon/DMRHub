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

// Package memdb gives tests their own SQLite databases.
//
// Use gives a test a SQLite file in WAL mode in its temporary directory,
// with the parameters that DMRHub uses by default, so that the test locks
// and checks foreign keys as a server does: a read never waits for a write.
// The file is a copy of a database that the process migrated once, so
// that each test does not run every migration again; UseEmpty gives an
// empty file to the tests of the migrations. UseSharedCache and DSN give a shared-cache memory database instead, for
// the tests of that mode. An empty SQLite name gives each connection of
// the pool its own empty database, so tables and rows made on one
// connection are missing on the next. A shared-cache name fixes that only
// while no other open database of the process has the same name. The
// names here are random, so no two tests share one, even under -count
// above one.
package memdb

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/configulator/v2"
	"gorm.io/gorm"
)

// Name returns a new SQLite name for a shared-cache memory database.
func Name() string {
	return "file:memdb_" + rand.Text()
}

// DSN returns a new SQLite DSN that opens a shared-cache memory database
// with foreign keys on, as DMRHub turns them on.
func DSN() string {
	return Name() + "?mode=memory&cache=shared&_pragma=foreign_keys(1)"
}

// Use points cfg at a new SQLite file in the temporary directory of t,
// with the default database parameters of DMRHub and params added to them.
// The file holds a copy of a database that the process migrated once, so
// db.MakeDB finds every migration done. Where the open files of the
// process can be listed, the test fails if the database is still open
// when its cleanups have run, since the space of an open file stays in
// use after the temporary directory is removed, which filled the disk of
// a fuzz run that opened one database for each input.
func Use(t testing.TB, cfg *config.Config, params ...string) {
	t.Helper()
	if err := UseIn(t.TempDir(), cfg, params...); err != nil {
		t.Fatal(err)
	}
	failIfLeftOpen(t, cfg.Database.Database)
}

// UseEmpty is Use with an empty file, for the tests of the migrations.
func UseEmpty(t testing.TB, cfg *config.Config, params ...string) {
	t.Helper()
	if err := pointAt(t.TempDir(), cfg, params...); err != nil {
		t.Fatal(err)
	}
	failIfLeftOpen(t, cfg.Database.Database)
}

// CloseAtCleanup closes the pool of gdb when the test ends, after the
// cleanups that the test registers later, which stop the work that uses it.
func CloseAtCleanup(t testing.TB, gdb *gorm.DB) {
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

func failIfLeftOpen(t testing.TB, path string) {
	t.Cleanup(func() {
		fds, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			return
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
			if err != nil {
				continue
			}
			target = strings.TrimSuffix(target, " (deleted)")
			if target == path || target == path+"-wal" || target == path+"-shm" {
				t.Errorf("the test left its database open: the process still has %s open after the cleanups of the test; close the *sql.DB of the *gorm.DB from db.MakeDB in t.Cleanup", target)
				return
			}
		}
	})
}

// UseIn is Use for a caller without a test, with the file in dir, which
// the caller removes when it closes the database.
func UseIn(dir string, cfg *config.Config, params ...string) error {
	if err := pointAt(dir, cfg, params...); err != nil {
		return err
	}
	data, err := migrated()
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfg.Database.Database, data, 0o600); err != nil {
		return fmt.Errorf("copy the migrated database: %w", err)
	}
	return nil
}

func pointAt(dir string, cfg *config.Config, params ...string) error {
	extra, err := defaults()
	if err != nil {
		return err
	}
	extra = slices.Clone(extra)
	for _, p := range params {
		if !slices.Contains(extra, p) {
			extra = append(extra, p)
		}
	}
	cfg.Database.Driver = config.DatabaseDriverSQLite
	cfg.Database.Database = filepath.Join(dir, "dmrhub.db")
	cfg.Database.ExtraParameters = extra
	return nil
}

// UseSharedCache points cfg at a new shared-cache SQLite memory database
// with foreign keys on, with params added to the extra parameters.
func UseSharedCache(cfg *config.Config, params ...string) {
	cfg.Database.Database = Name()
	cfg.Database.ExtraParameters = append([]string{"mode=memory", "cache=shared", "_pragma=foreign_keys(1)"}, params...)
}

func defaults() ([]string, error) {
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		return nil, fmt.Errorf("read the default database parameters: %w", err)
	}
	return cfg.Database.ExtraParameters, nil
}

//nolint:gochecknoglobals // the migrated database that the tests of the process copy
var migrated = sync.OnceValues(func() ([]byte, error) {
	dir, err := os.MkdirTemp("", "dmrhub-migrated-")
	if err != nil {
		return nil, fmt.Errorf("make the directory of the migrated database: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		return nil, fmt.Errorf("read the default config: %w", err)
	}
	if err := pointAt(dir, &cfg); err != nil {
		return nil, err
	}
	gdb, err := db.MakeDB(&cfg)
	if err != nil {
		return nil, fmt.Errorf("migrate the database to copy: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("get the pool of the migrated database: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return nil, fmt.Errorf("close the migrated database: %w", err)
	}
	if _, err := os.Stat(cfg.Database.Database + "-wal"); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("the migrated database kept its WAL: %w", err)
	}
	data, err := os.ReadFile(cfg.Database.Database)
	if err != nil {
		return nil, fmt.Errorf("read the migrated database: %w", err)
	}
	return data, nil
})
