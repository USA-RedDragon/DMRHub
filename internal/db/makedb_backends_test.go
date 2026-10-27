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
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type schemaNet struct {
	ID          uint `gorm:"primaryKey"`
	TalkgroupID uint
	Active      bool
	StartTime   time.Time
}

func (schemaNet) TableName() string { return "nets" }

type schemaUser struct {
	ID       uint `gorm:"primaryKey"`
	Callsign string
	Username string
}

func (schemaUser) TableName() string { return "users" }

type schemaCall struct {
	ID       uint `gorm:"primaryKey"`
	CallData []byte
	StreamID uint
}

func (schemaCall) TableName() string { return "calls" }

func backendConfig(t *testing.T, be testutils.Backend) config.Config {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	be.Setup(t, &cfg)
	if cfg.Database.Driver == config.DatabaseDriverSQLite {
		cfg.Database.Database = filepath.Join(t.TempDir(), "dmrhub.db")
		cfg.Database.ExtraParameters = nil
	}
	return cfg
}

func checkSchema(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	require.NoError(t, gdb.Create(&schemaUser{ID: 1, Callsign: "N0CALL", Username: "admin"}).Error)
	require.NoError(t, gdb.Create(&schemaUser{ID: 2, Callsign: "n0call", Username: "Admin"}).Error, "callsigns and usernames are unique by their bytes")
	require.Error(t, gdb.Create(&schemaUser{ID: 3, Callsign: "N0CALL", Username: "other"}).Error)

	burst := make([]byte, 33*10000)
	for i := range burst {
		burst[i] = byte(i)
	}
	require.NoError(t, gdb.Create(&schemaCall{ID: 1, CallData: burst}).Error)
	var call schemaCall
	require.NoError(t, gdb.First(&call, 1).Error)
	assert.Equal(t, burst, call.CallData, "a call keeps all of its octets")

	zero := schemaNet{TalkgroupID: 8}
	require.NoError(t, gdb.Create(&zero).Error, "a zero time is stored")
	var got schemaNet
	require.NoError(t, gdb.First(&got, zero.ID).Error)
	assert.True(t, got.StartTime.IsZero(), "a zero time reads back as zero, not %v", got.StartTime)

	now := time.Now()
	require.NoError(t, gdb.Create(&schemaNet{TalkgroupID: 9, Active: false, StartTime: now}).Error)
	require.NoError(t, gdb.Create(&schemaNet{TalkgroupID: 9, Active: false, StartTime: now}).Error)
	require.NoError(t, gdb.Create(&schemaNet{TalkgroupID: 9, Active: true, StartTime: now}).Error)
	require.Error(t, gdb.Create(&schemaNet{TalkgroupID: 9, Active: true, StartTime: now}).Error, "a talkgroup has at most one active net")
	require.NoError(t, gdb.Create(&schemaNet{TalkgroupID: 10, Active: true, StartTime: now}).Error)
}

func makeDBFresh(t *testing.T, be testutils.Backend) {
	t.Helper()
	cfg := backendConfig(t, be)
	gdb, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	closeDB(t, gdb)
	gdb, err = db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(t, gdb) })
	if cfg.Database.Driver == config.DatabaseDriverMySQL {
		var tables []string
		require.NoError(t, gdb.Raw("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_COLLATION <> 'utf8mb4_bin'").Scan(&tables).Error)
		assert.Empty(t, tables, "tables that do not compare strings by their bytes")
	}
	checkSchema(t, gdb)
}

// TestMakeDBOnMySQL makes a fresh database on MySQL and then opens it again.
func TestMakeDBOnMySQL(t *testing.T) {
	t.Parallel()
	makeDBFresh(t, testutils.MySQLBackend())
}

// TestMakeDBOnMariaDB makes a fresh database on MariaDB and then opens it
// again.
func TestMakeDBOnMariaDB(t *testing.T) {
	t.Parallel()
	makeDBFresh(t, testutils.MariaDBBackend())
}

type recorder struct {
	logger.Interface
	mu  sync.Mutex
	sql []string
}

func (r *recorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	s, _ := fc()
	r.mu.Lock()
	r.sql = append(r.sql, s)
	r.mu.Unlock()
}

var ddl = regexp.MustCompile(`(?i)^\s*(ALTER|CREATE|DROP|RENAME)\s`)

// newerSchema matches the DDL of the schema changes made after 2026-10-26:
// the plugin names unique per built-in key, the Setup settings, the
// generated secrets and the verified email addresses.
var newerSchema = regexp.MustCompile("(?i)idx_plugins_name|idx_plugins_builtin_name|setup_settings|generated_secrets|email_verified|email_verifications")

func (r *recorder) ddl() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, s := range r.sql {
		if ddl.MatchString(s) {
			out = append(out, s)
		}
	}
	return out
}

func loadSchema(t *testing.T, gdb *gorm.DB, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "schema-2026-10-26", name+".sql"))
	require.NoError(t, err)
	require.NoError(t, gdb.Connection(func(conn *gorm.DB) error {
		if conn.Name() == "mysql" {
			if err := conn.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
				return err
			}
		}
		for _, stmt := range strings.Split(string(data), ";\n") {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if err := conn.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	}))
}

// TestMakeDBUpgradesTheSchemaOf20261026 loads the schema that MakeDB made
// on 2026-10-26 on each database, with some rows, and then makes the
// database. On MySQL and MariaDB that schema is what was left when MakeDB
// failed. The rows stay, and on Postgres and SQLite no column changes.
func TestMakeDBUpgradesTheSchemaOf20261026(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg := backendConfig(t, be)
			rec := &recorder{Interface: logger.Discard}
			gdb, err := gorm.Open(db.Dialect(&cfg), &gorm.Config{Logger: rec})
			require.NoError(t, err)
			t.Cleanup(func() { closeDB(t, gdb) })
			loadSchema(t, gdb, be.Name)

			burst := []byte{0, 1, 2, 0xff, 0xfe}
			hasUsers, hasCalls := gdb.Migrator().HasTable("users"), gdb.Migrator().HasTable("calls")
			if hasUsers {
				require.NoError(t, gdb.Create(&schemaUser{ID: 7, Callsign: "W1AW", Username: "w1aw"}).Error)
			}
			if hasCalls {
				require.NoError(t, gdb.Create(&schemaCall{ID: 7, CallData: burst, StreamID: 99}).Error)
			}
			rec.mu.Lock()
			rec.sql = nil
			rec.mu.Unlock()

			require.NoError(t, db.Migrate(gdb, &cfg))
			changes := rec.ddl()
			for _, s := range changes {
				t.Logf("DDL: %s", s)
			}
			if cfg.Database.Driver != config.DatabaseDriverMySQL {
				var old []string
				for _, s := range changes {
					if !newerSchema.MatchString(s) {
						old = append(old, s)
					}
				}
				assert.Empty(t, old, "the schema of 2026-10-26 needs no change but the newer ones")
			}

			if hasUsers {
				var u schemaUser
				require.NoError(t, gdb.First(&u, 7).Error)
				assert.Equal(t, schemaUser{ID: 7, Callsign: "W1AW", Username: "w1aw"}, u)
				require.NoError(t, gdb.Delete(&schemaUser{}, 7).Error)
			}
			if hasCalls {
				var c schemaCall
				require.NoError(t, gdb.First(&c, 7).Error)
				assert.Equal(t, schemaCall{ID: 7, CallData: burst, StreamID: 99}, c)
				require.NoError(t, gdb.Delete(&schemaCall{}, 7).Error)
			}

			again, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() { closeDB(t, again) })
			checkSchema(t, again)
		})
	}
}
