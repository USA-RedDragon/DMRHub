// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package testutils

import (
	"context"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TestDB struct {
	database *gorm.DB
}

func (t *TestDB) CloseDB() {
	if t.database != nil {
		sqlDB, _ := t.database.DB()
		_ = sqlDB.Close()
	}
	t.database = nil
}

func CreateTestDBRouter() (*gin.Engine, *TestDB, error) {
	var t TestDB
	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create default config: %w", err)
	}

	defConfig.Database.Database = "" // Use in-memory database for tests
	defConfig.Database.ExtraParameters = []string{}

	t.database, err = db.MakeDB(&defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database: %w", err)
	}

	adminCount := int64(0)
	err = t.database.Model(&models.User{}).Where("username = ?", "Admin").Count(&adminCount).Error
	if err != nil {
		return nil, nil, fmt.Errorf("failed to count admin users: %w", err)
	}
	if adminCount < 1 {
		err = t.database.Create(&models.User{
			Username:   "Admin",
			Password:   utils.HashPassword("password", defConfig.PasswordSalt),
			Admin:      true,
			SuperAdmin: true,
			Callsign:   "XXXXXX",
			Approved:   true,
		}).Error
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create admin user: %w", err)
		}
	}

	pubsub, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	return http.CreateRouter(&defConfig, t.database, pubsub, "test", "deadbeef"), &t, nil
}
