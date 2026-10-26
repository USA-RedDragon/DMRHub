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
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"strings"

	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/consts"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/glebarez/sqlite"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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
		if len(config.Database.ExtraParameters) > 0 {
			extraParamsStr = "&" + config.Database.ExtraParameters[0]
			for _, param := range config.Database.ExtraParameters[1:] {
				extraParamsStr += "&" + param
			}
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
		if len(config.Database.ExtraParameters) > 0 {
			for _, param := range config.Database.ExtraParameters {
				dsn += " " + param
			}
		}
		dialector = postgres.New(postgres.Config{
			DSN: dsn,
		})
	}
	return dialector
}

func sqliteParameters(params []string) []string {
	if slices.ContainsFunc(params, func(p string) bool {
		return strings.HasPrefix(p, "_txlock=") || p == "cache=shared"
	}) {
		return params
	}
	return append(slices.Clone(params), "_txlock=immediate")
}

func MakeDB(config *configPkg.Config) (db *gorm.DB, err error) {
	db, err = gorm.Open(getDialect(config))
	if err != nil {
		return db, fmt.Errorf("failed to open database: %w", err)
	}

	if config.Metrics.OTLPEndpoint != "" {
		if err = db.Use(otelgorm.NewPlugin()); err != nil {
			return db, fmt.Errorf("failed to trace database: %w", err)
		}
	}

	unlock, err := lockMigrations(db, config)
	if err != nil {
		return db, err
	}
	err = migrate(db, config)
	if unlockErr := unlock(); err == nil {
		err = unlockErr
	}
	if err != nil {
		return db, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return db, fmt.Errorf("failed to open database: %w", err)
	}
	sqlDB.SetMaxOpenConns(runtime.GOMAXPROCS(0) * consts.ConnsPerCPU)
	sqlDB.SetMaxIdleConns(runtime.GOMAXPROCS(0) * consts.ConnsPerCPU)
	sqlDB.SetConnMaxIdleTime(consts.MaxIdleTime)

	return
}

const migrationLockKey int64 = 0x444d524875624d67

func lockMigrations(db *gorm.DB, config *configPkg.Config) (func() error, error) {
	if config.Database.Driver != configPkg.DatabaseDriverPostgres {
		return func() error { return nil }, nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to take a connection for the migration lock: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to take the migration lock: %w", err)
	}
	return func() error {
		_, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockKey)
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
	err := migration.Migrate(db, config)
	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	err = db.AutoMigrate(&models.AppSettings{}, &models.Call{}, &models.Peer{}, &models.PeerRule{}, &models.Repeater{}, &models.Ratelimit{}, &models.Talkgroup{}, &models.User{}, &models.Message{}, &models.UserRadioID{}, &models.Session{}, &models.OAuthGrant{}, &models.OAuthCode{}, &models.OAuthRefreshToken{}, &models.PositionSetting{}, &models.Position{}, &models.Plugin{}, &models.PluginRange{}, &models.PasswordReset{})
	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// Grab the first (and only) AppSettings record. If that record doesn't exist, create it.
	var appSettings models.AppSettings
	result := db.First(&appSettings)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to query app settings: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		slog.Debug("App settings entry doesn't exist, creating it")
		// The record doesn't exist, so create it
		appSettings = models.AppSettings{
			HasSeeded: false,
		}
		err = db.Create(&appSettings).Error
		if err != nil {
			return fmt.Errorf("failed to create app settings: %w", err)
		}
		slog.Debug("App settings created")
	}

	// If the record exists and HasSeeded is true, then we don't need to seed the database.
	if !appSettings.HasSeeded {
		appSettings.HasSeeded = true
		err = db.Save(&appSettings).Error
		if err != nil {
			return fmt.Errorf("failed to save app settings: %w", err)
		}
	}

	return nil
}
