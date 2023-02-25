// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"os"
	"runtime"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/glebarez/sqlite"
	gorm_seeder "github.com/kachit/gorm-seeder"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func MakeDB() *gorm.DB {
	var db *gorm.DB
	var err error
	if os.Getenv("TEST") != "" {
		logging.GetLogger(logging.Error).Log(MakeDB, "Using in-memory database for testing")
		db, err = gorm.Open(sqlite.Open(""), &gorm.Config{})
		if err != nil {
			logging.GetLogger(logging.Error).Logf(MakeDB, "Could not open database: %s", err)
			os.Exit(1)
		}
	} else {
		db, err = gorm.Open(postgres.Open(config.GetConfig().PostgresDSN), &gorm.Config{})
		if err != nil {
			logging.GetLogger(logging.Error).Logf(MakeDB, "Could not open database: %s", err)
			os.Exit(1)
		}
		if config.GetConfig().OTLPEndpoint != "" {
			if err = db.Use(otelgorm.NewPlugin()); err != nil {
				logging.GetLogger(logging.Error).Logf(MakeDB, "Could not trace database: %s", err)
				os.Exit(1)
			}
		}
	}

	err = migration.Migrate(db)
	if err != nil {
		logging.GetLogger(logging.Error).Logf(MakeDB, "Could not migrate database: %s", err)
		os.Exit(1)
	}

	err = db.AutoMigrate(&models.AppSettings{}, &models.Call{}, &models.Repeater{}, &models.Talkgroup{}, &models.User{})
	if err != nil {
		logging.GetLogger(logging.Error).Logf(MakeDB, "Could not migrate database: %s", err)
		os.Exit(1)
	}

	// Grab the first (and only) AppSettings record. If that record doesn't exist, create it.
	var appSettings models.AppSettings
	result := db.First(&appSettings)
	if result.RowsAffected == 0 {
		if config.GetConfig().Debug {
			logging.GetLogger(logging.Error).Log(MakeDB, "App settings entry doesn't exist, creating it")
		}
		// The record doesn't exist, so create it
		appSettings = models.AppSettings{
			HasSeeded: false,
		}
		err := db.Create(&appSettings).Error
		if err != nil {
			logging.GetLogger(logging.Error).Logf(MakeDB, "Failed to create app settings: %s", err)
			os.Exit(1)
		}
		if config.GetConfig().Debug {
			logging.GetLogger(logging.Error).Log(MakeDB, "App settings saved")
		}
	}

	// If the record exists and HasSeeded is true, then we don't need to seed the database.
	if !appSettings.HasSeeded {
		usersSeeder := models.NewUsersSeeder(gorm_seeder.SeederConfiguration{Rows: models.UserSeederRows})
		talkgroupsSeeder := models.NewTalkgroupsSeeder(gorm_seeder.SeederConfiguration{Rows: models.TalkgroupSeederRows})
		seedersStack := gorm_seeder.NewSeedersStack(db)
		seedersStack.AddSeeder(&usersSeeder)
		seedersStack.AddSeeder(&talkgroupsSeeder)

		// Apply seed
		err = seedersStack.Seed()
		if err != nil {
			logging.GetLogger(logging.Error).Logf(MakeDB, "Failed to seed database: %s", err)
			os.Exit(1)
		}
		appSettings.HasSeeded = true
		err := db.Save(&appSettings).Error
		if err != nil {
			logging.GetLogger(logging.Error).Logf(MakeDB, "Failed to save app settings: %s", err)
			os.Exit(1)
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		logging.GetLogger(logging.Error).Logf(MakeDB, "Failed to open database: %s", err)
		os.Exit(1)
	}
	sqlDB.SetMaxIdleConns(runtime.GOMAXPROCS(0))
	const connsPerCPU = 10
	sqlDB.SetMaxOpenConns(runtime.GOMAXPROCS(0) * connsPerCPU)
	const maxIdleTime = 10 * time.Minute
	sqlDB.SetConnMaxIdleTime(maxIdleTime)

	return db
}
