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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/consts"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/glebarez/sqlite"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var gormLogWriter logger.Writer = log.New(os.Stdout, "\r\n", log.LstdFlags) //nolint:gochecknoglobals // a test points it at a buffer

func gormLogger() logger.Interface {
	return logger.New(gormLogWriter, logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  true,
	})
}

func getDialect(config *configPkg.Config) gorm.Dialector {
	var dialector gorm.Dialector
	switch config.Database.Driver {
	case configPkg.DatabaseDriverSQLite:
		dsn := config.Database.Database
		params := config.Database.ExtraParameters
		if dsn != "" {
			params = sqliteParameters(params)
		}
		if len(params) > 0 {
			dsn += "?" + strings.Join(params, "&")
		}
		dialector = sqlite.Open(dsn)
	case configPkg.DatabaseDriverMySQL:
		hasUser := config.Database.Username != ""
		hasPassword := config.Database.Password != ""
		hasUserAndPassword := hasUser && hasPassword
		prefix := ""
		switch {
		case hasUserAndPassword:
			prefix = fmt.Sprintf("%s:%s@", config.Database.Username, config.Database.Password)
		case hasUser:
			prefix = fmt.Sprintf("%s@", config.Database.Username)
		case hasPassword:
			prefix = fmt.Sprintf(":%s@", config.Database.Password)
		}
		portStr := ""
		if config.Database.Port != 0 {
			portStr = fmt.Sprintf(":%d", config.Database.Port)
		}
		extraParamsStr := ""
		for _, param := range mysqlParameters(serverParameters(config.Database.ExtraParameters)) {
			extraParamsStr += "&" + param
		}
		dsn := fmt.Sprintf("%stcp(%s%s)/%s?charset=utf8mb4&parseTime=True&loc=Local%s",
			prefix,
			config.Database.Host,
			portStr,
			config.Database.Database,
			extraParamsStr)
		dialector = mysql.Open(dsn)
	case configPkg.DatabaseDriverPostgres:
		dsn := "host=" + config.Database.Host + " dbname=" + config.Database.Database
		if config.Database.Port != 0 {
			dsn += fmt.Sprintf(" port=%d", config.Database.Port)
		}
		if config.Database.Username != "" {
			dsn += " user=" + config.Database.Username
		}
		if config.Database.Password != "" {
			dsn += " password=" + config.Database.Password
		}
		for _, param := range serverParameters(config.Database.ExtraParameters) {
			dsn += " " + param
		}
		dialector = postgres.New(postgres.Config{
			DSN: dsn,
		})
	}
	return dialector
}

func serverParameters(params []string) []string {
	return slices.DeleteFunc(slices.Clone(params), func(p string) bool {
		return strings.HasPrefix(p, "_pragma=")
	})
}

// sqliteSynchronous is the synchronous pragma of a SQLite file. In WAL
// mode, NORMAL does not wait for the disk on each commit, only at a
// checkpoint: a power cut can lose the last commits, but the database stays
// consistent.
const sqliteSynchronous = "_pragma=synchronous(NORMAL)"

// mysqlSQLMode is the default sql_mode of MySQL 8.4 without NO_ZERO_DATE
// and NO_ZERO_IN_DATE, since the driver writes a zero time.Time as
// 0000-00-00 and reads it back as the zero time.
const mysqlSQLMode = "sql_mode=%27ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION%27"

func mysqlParameters(params []string) []string {
	if slices.ContainsFunc(params, func(p string) bool { return strings.HasPrefix(p, "sql_mode=") }) {
		return params
	}
	return append(slices.Clone(params), mysqlSQLMode)
}

func sqliteParameters(params []string) []string {
	out := slices.Clone(params)
	if !slices.ContainsFunc(params, func(p string) bool {
		return strings.HasPrefix(strings.ToLower(p), "_pragma=synchronous")
	}) {
		out = append(out, sqliteSynchronous)
	}
	if slices.ContainsFunc(params, func(p string) bool {
		return strings.HasPrefix(p, "_txlock=") || p == "cache=shared"
	}) {
		return out
	}
	return append(out, "_txlock=immediate")
}

func sqliteWritesInTurn(config *configPkg.Config) bool {
	if config.Database.Driver != configPkg.DatabaseDriverSQLite || config.Database.Database == "" {
		return false
	}
	params := sqliteParameters(config.Database.ExtraParameters)
	return slices.Contains(params, "_txlock=immediate") || slices.Contains(params, "cache=shared")
}

// MakeDB connects to the database of a config and migrates it.
func MakeDB(config *configPkg.Config) (*gorm.DB, error) {
	return MakeDBContext(context.Background(), config)
}

// MakeDBContext connects to the database of a config and migrates it. It
// returns the error of ctx when ctx ends before the migration starts, and
// closes the database when it returns an error. The end of ctx does not
// stop a migration that started; see Connection.Migrate.
func MakeDBContext(ctx context.Context, config *configPkg.Config) (*gorm.DB, error) {
	c, err := Connect(ctx, config)
	if err != nil {
		return nil, err
	}
	return c.Migrate(ctx)
}

// Connection is a database whose migration lock is held, so that it can
// migrate.
type Connection struct {
	db     *gorm.DB
	pool   *sql.DB
	config *configPkg.Config
	unlock func() error
}

// Connect connects to the database of a config and takes the lock that
// lets one replica migrate at a time. It returns the error of ctx once ctx
// ends, and closes the database when it returns an error.
func Connect(ctx context.Context, config *configPkg.Config) (*Connection, error) {
	db, err := gorm.Open(getDialect(config), &gorm.Config{Logger: gormLogger(), DisableAutomaticPing: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	c, err := connect(ctx, db, config)
	if err != nil {
		closeDB(db)
		return nil, err
	}
	return c, nil
}

// Ping connects to the database of a config, reads from it and closes
// it. It takes no lock and migrates nothing. A SQLite file that did not
// exist before Ping does not exist after it either.
func Ping(ctx context.Context, config *configPkg.Config) error {
	made := newSQLiteFile(config)
	db, err := gorm.Open(getDialect(config), &gorm.Config{Logger: gormLogger(), DisableAutomaticPing: true})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() {
		closeDB(db)
		if made != "" {
			for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
				_ = os.Remove(made + suffix)
			}
		}
	}()
	pool, ok := db.ConnPool.(*sql.DB)
	if !ok {
		return fmt.Errorf("failed to open database: unexpected connection pool %T", db.ConnPool)
	}
	var one int
	if err := pool.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	if config.Database.Driver == configPkg.DatabaseDriverSQLite {
		var version int
		if err := pool.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&version); err != nil {
			return fmt.Errorf("failed to read database: %w", err)
		}
	}
	return nil
}

// newSQLiteFile returns the path of the SQLite file of a config when that
// file does not exist yet, and "" for any other database.
func newSQLiteFile(config *configPkg.Config) string {
	d := config.Database
	if d.Driver != configPkg.DatabaseDriverSQLite || slices.Contains(d.ExtraParameters, "mode=memory") {
		return ""
	}
	path := strings.TrimPrefix(d.Database, "file:")
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" || path == ":memory:" {
		return ""
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return ""
	}
	return path
}

func connect(ctx context.Context, db *gorm.DB, config *configPkg.Config) (*Connection, error) {
	pool, ok := db.ConnPool.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("failed to open database: unexpected connection pool %T", db.ConnPool)
	}
	if err := pool.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if sqliteWritesInTurn(config) {
		writers := newSQLiteWriters(pool)
		db.ConnPool, db.Statement.ConnPool = writers, writers
	}

	if config.Metrics.OTLPEndpoint != "" {
		if err := db.Use(otelgorm.NewPlugin()); err != nil {
			return nil, fmt.Errorf("failed to trace database: %w", err)
		}
	}

	unlock, err := lockMigrations(ctx, db, config)
	if err != nil {
		return nil, err
	}
	return &Connection{db: db, pool: pool, config: config, unlock: unlock}, nil
}

// Migrate migrates the database, releases the migration lock and returns
// the database. The end of ctx does not stop it, since a migration that
// stops halfway can leave a database that no version of DMRHub reads. It
// closes the database when it returns an error.
func (c *Connection) Migrate(ctx context.Context) (*gorm.DB, error) {
	err := migrate(c.db.WithContext(context.WithoutCancel(ctx)), c.config)
	if unlockErr := c.unlock(); err == nil {
		err = unlockErr
	}
	if err != nil {
		closeDB(c.db)
		return nil, err
	}
	c.pool.SetMaxOpenConns(runtime.GOMAXPROCS(0) * consts.ConnsPerCPU)
	c.pool.SetMaxIdleConns(runtime.GOMAXPROCS(0) * consts.ConnsPerCPU)
	c.pool.SetConnMaxIdleTime(consts.MaxIdleTime)
	return c.db, nil
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

const (
	migrationLockKey  int64 = 0x444d524875624d67
	migrationLockName       = "dmrhub_migrations:"
	mysqlLockWait           = 60
)

var errMigrationLock = errors.New("the database refused the migration lock")

// lockMigrations takes a lock that the replicas share on the database, so
// that one replica migrates at a time. It holds a connection of its own for
// the lock, since both locks belong to the session that took them.
func lockMigrations(ctx context.Context, db *gorm.DB, config *configPkg.Config) (func() error, error) {
	var lock, unlock func(ctx context.Context, conn *sql.Conn) error
	switch config.Database.Driver {
	case configPkg.DatabaseDriverPostgres:
		lock = func(ctx context.Context, conn *sql.Conn) error {
			if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
				return fmt.Errorf("pg_advisory_lock: %w", err)
			}
			return nil
		}
		unlock = func(ctx context.Context, conn *sql.Conn) error {
			if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockKey); err != nil {
				return fmt.Errorf("pg_advisory_unlock: %w", err)
			}
			return nil
		}
	case configPkg.DatabaseDriverMySQL:
		lock = func(ctx context.Context, conn *sql.Conn) error {
			for {
				var got sql.NullInt64
				if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(LEFT(CONCAT(?, DATABASE()), 64), ?)", migrationLockName, mysqlLockWait).Scan(&got); err != nil {
					return fmt.Errorf("GET_LOCK: %w", err)
				}
				if !got.Valid {
					return errMigrationLock
				}
				if got.Int64 == 1 {
					return nil
				}
			}
		}
		unlock = func(ctx context.Context, conn *sql.Conn) error {
			if _, err := conn.ExecContext(ctx, "SELECT RELEASE_LOCK(LEFT(CONCAT(?, DATABASE()), 64))", migrationLockName); err != nil {
				return fmt.Errorf("RELEASE_LOCK: %w", err)
			}
			return nil
		}
	case configPkg.DatabaseDriverSQLite:
		return func() error { return nil }, nil
	default:
		return func() error { return nil }, nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to take a connection for the migration lock: %w", err)
	}
	if err := lock(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to take the migration lock: %w", err)
	}
	return func() error {
		err := unlock(context.WithoutCancel(ctx), conn)
		closeErr := conn.Close()
		if err != nil {
			return fmt.Errorf("failed to release the migration lock: %w", err)
		}
		if closeErr != nil {
			return fmt.Errorf("failed to close the migration lock connection: %w", closeErr)
		}
		return nil
	}, nil
}

func migrate(db *gorm.DB, config *configPkg.Config) error {
	if config.Database.Driver == configPkg.DatabaseDriverMySQL {
		db = db.Set("gorm:table_options", " DEFAULT CHARSET=utf8mb4 COLLATE="+migration.MySQLCollation).Session(&gorm.Session{})
	}
	err := migration.Migrate(db, config)
	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	err = db.AutoMigrate(&models.Call{}, &models.Peer{}, &models.PeerRule{}, &models.Repeater{}, &models.Ratelimit{}, &models.Talkgroup{}, &models.User{}, &models.Message{}, &models.UserRadioID{}, &models.Session{}, &models.OAuthGrant{}, &models.OAuthCode{}, &models.OAuthRefreshToken{}, &models.PersonalToken{}, &models.PositionSetting{}, &models.Position{}, &models.Plugin{}, &models.PluginRange{}, &models.PasswordReset{}, &models.EmailVerification{}, &models.SetupSettings{}, &models.GeneratedSecret{}, &models.FirstAdmin{}, &models.RadioIDDrift{}, &models.RoutingRule{}, &models.LoginFailure{}, &models.Invite{}, &models.MessageAccess{}, &models.MessageAccessRadioID{}, &models.CallPart{}, &models.RepeaterCosysop{}, &models.RepeaterEvent{}, &models.OfflineAlert{}, &models.HeldMessage{}, &models.LocalGroup{}, &models.LocalGroupMember{}, &models.PluginStoreEntry{}, &models.PluginSwitch{}, &models.PluginUserSwitch{})
	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	if err := migration.MigrateSchema(db, config); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	return nil
}
