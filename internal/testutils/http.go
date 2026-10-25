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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type TestDB struct {
	database *gorm.DB
	pubsub   pubsub.PubSub
	hub      *hub.Hub
	config   *config.Config
}

func (t *TestDB) CloseDB() {
	if t.database != nil {
		sqlDB, _ := t.database.DB()
		_ = sqlDB.Close()
	}
	t.database = nil
}

// PubSub returns the pubsub of the router of CreateTestDBRouterWithOptions.
func (t *TestDB) PubSub() pubsub.PubSub {
	return t.pubsub
}

// Hub returns the hub of the router, or nil when it has none.
func (t *TestDB) Hub() *hub.Hub {
	return t.hub
}

func (t *TestDB) DB() *gorm.DB {
	return t.database
}

//nolint:gochecknoglobals
var memoryDBs atomic.Uint64

func useMemoryDB(cfg *config.Config) {
	cfg.Database.Database = fmt.Sprintf("file:testdb_%d", memoryDBs.Add(1))
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared", "_pragma=foreign_keys(1)"}
}

func CreateTestDBRouter() (*gin.Engine, *TestDB, error) {
	return CreateTestDBRouterWithConfig(nil)
}

// CreateTestDBRouterWithConfig is CreateTestDBRouter with the config changed
// by change before the router is made, and the router made with opts.
func CreateTestDBRouterWithConfig(change func(*config.Config), opts ...http.Option) (*gin.Engine, *TestDB, error) {
	var t TestDB
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create default config: %w", err)
	}

	useMemoryDB(&defConfig)
	defConfig.DMR.OpenBridge.Enabled = true
	defConfig.DMR.IPSC.Enabled = true
	if change != nil {
		change(&defConfig)
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

	pubsub, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create pubsub: %w", err)
	}
	t.pubsub = pubsub

	ready := &atomic.Bool{}
	ready.Store(true)

	return http.CreateRouter(context.TODO(), &defConfig, nil, t.database, pubsub, ready, nil, "test", "deadbeef", opts...), &t, nil
}

// CreateTestDBRouterWithHub creates a test router with a hub. Its in-memory
// database is shared by every connection, so the goroutines of the hub see
// what the handlers write.
func CreateTestDBRouterWithHub() (*gin.Engine, *TestDB, error) {
	var t TestDB
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create default config: %w", err)
	}

	useMemoryDB(&defConfig)
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

	ct := calltracker.NewCallTracker(t.database, ps, kvStore)
	dmrHub := hub.NewHub(t.database, kvStore, ps, ct)
	t.hub = dmrHub

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
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create default config: %w", err)
	}

	useMemoryDB(&defConfig)
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

	t.pubsub = ps
	t.config = &defConfig
	ct := calltracker.NewCallTracker(t.database, ps, kvStore)
	dmrHub := hub.NewHub(t.database, kvStore, ps, ct)
	t.hub = dmrHub

	ready := &atomic.Bool{}
	ready.Store(true)

	return http.CreateRouter(context.TODO(), &defConfig, dmrHub, t.database, ps, ready, nil, "test", "deadbeef"), &t, nil
}

// ReplicaRouter returns the router of a second copy of the app on the
// database of the router of CreateTestDBRouterWithOptions, with a Hub of its
// own. With Redis on it has its own pubsub, so the two routers reach each
// other only through Redis; with the in-memory pubsub it shares the first
// router's, as SpawnSecondReplica does.
func (t *TestDB) ReplicaRouter(tb testing.TB) *gin.Engine {
	tb.Helper()
	require.NotNil(tb, t.config, "ReplicaRouter needs the router of CreateTestDBRouterWithOptions")
	ps := t.pubsub
	if t.config.Redis.Enabled {
		var err error
		ps, err = pubsub.MakePubSub(context.TODO(), t.config)
		require.NoError(tb, err)
		tb.Cleanup(func() { _ = ps.Close() })
	}
	kvStore, err := kv.MakeKV(context.TODO(), t.config)
	require.NoError(tb, err)
	ct := calltracker.NewCallTracker(t.database, ps, kvStore)
	dmrHub := hub.NewHub(t.database, kvStore, ps, ct)
	tb.Cleanup(func() {
		dmrHub.Stop(context.Background())
		ct.Stop()
		_ = kvStore.Close()
	})
	ready := &atomic.Bool{}
	ready.Store(true)
	return http.CreateRouter(context.TODO(), t.config, dmrHub, t.database, ps, ready, nil, "test", "deadbeef")
}
