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

package testutils

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
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

func (t *TestDB) DB() *gorm.DB {
	return t.database
}

func CreateTestDBRouter() (*gin.Engine, *TestDB, error) {
	var t TestDB
	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create default config: %w", err)
	}

	defConfig.Database.Database = "" // Use in-memory database for tests
	defConfig.Database.ExtraParameters = []string{}
	defConfig.DMR.OpenBridge.Enabled = true
	defConfig.DMR.IPSC.Enabled = true

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
		hashedPassword, hashErr := utils.HashPassword("password", defConfig.PasswordSalt)
		if hashErr != nil {
			return nil, nil, fmt.Errorf("failed to hash password: %w", hashErr)
		}
		err = t.database.Create(&models.User{
			Username:   "Admin",
			Password:   hashedPassword,
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

	ready := &atomic.Bool{}
	ready.Store(true)

	return http.CreateRouter(context.TODO(), &defConfig, nil, t.database, pubsub, ready, nil, "test", "deadbeef"), &t, nil
}

func CreateTestDBRouterWithHub() (*gin.Engine, *TestDB, error) {
	var t TestDB
	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create default config: %w", err)
	}

	defConfig.Database.Database = "" // Use in-memory database for tests
	defConfig.Database.ExtraParameters = []string{}
	defConfig.DMR.DisableRadioIDValidation = true
	defConfig.DMR.OpenBridge.Enabled = true
	defConfig.DMR.IPSC.Enabled = true

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
		hashedPassword, hashErr := utils.HashPassword("password", defConfig.PasswordSalt)
		if hashErr != nil {
			return nil, nil, fmt.Errorf("failed to hash password: %w", hashErr)
		}
		err = t.database.Create(&models.User{
			Username:   "Admin",
			Password:   hashedPassword,
			Admin:      true,
			SuperAdmin: true,
			Callsign:   "XXXXXX",
			Approved:   true,
		}).Error
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create admin user: %w", err)
		}
	}

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kv: %w", err)
	}

	ct := calltracker.NewCallTracker(t.database, ps)
	dmrHub := hub.NewHub(t.database, kvStore, ps, ct)

	ready := &atomic.Bool{}
	ready.Store(true)

	return http.CreateRouter(context.TODO(), &defConfig, dmrHub, t.database, ps, ready, nil, "test", "deadbeef"), &t, nil
}

// ConfigOption is a function that mutates a config before the test router is created.
type ConfigOption func(*config.Config)

// CreateTestDBRouterWithOptions creates a test DB router and applies optional
// config overrides. This is useful for testing behavior when specific features
// (e.g. OpenBridge, IPSC) are disabled.
func CreateTestDBRouterWithOptions(opts ...ConfigOption) (*gin.Engine, *TestDB, error) {
	var t TestDB
	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create default config: %w", err)
	}

	defConfig.Database.Database = "" // Use in-memory database for tests
	defConfig.Database.ExtraParameters = []string{}
	defConfig.DMR.OpenBridge.Enabled = true
	defConfig.DMR.IPSC.Enabled = true
	defConfig.DMR.DisableRadioIDValidation = true

	for _, opt := range opts {
		opt(&defConfig)
	}

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
		hashedPassword, hashErr := utils.HashPassword("password", defConfig.PasswordSalt)
		if hashErr != nil {
			return nil, nil, fmt.Errorf("failed to hash password: %w", hashErr)
		}
		err = t.database.Create(&models.User{
			Username:   "Admin",
			Password:   hashedPassword,
			Admin:      true,
			SuperAdmin: true,
			Callsign:   "XXXXXX",
			Approved:   true,
		}).Error
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create admin user: %w", err)
		}
	}

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kv: %w", err)
	}

	ct := calltracker.NewCallTracker(t.database, ps)
	dmrHub := hub.NewHub(t.database, kvStore, ps, ct)

	ready := &atomic.Bool{}
	ready.Store(true)

	return http.CreateRouter(context.TODO(), &defConfig, dmrHub, t.database, ps, ready, nil, "test", "deadbeef"), &t, nil
}
