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
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"runtime"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/gin-gonic/gin"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type TestDB struct {
	client         *redis.Client
	database       *gorm.DB
	redisContainer *dockertest.Resource
}

func (t *TestDB) createRedis() *redis.Client {
	if t.client != nil {
		return t.client
	}
	pool, err := dockertest.NewPool("")
	if err != nil {
		logging.Errorf("Could not construct pool: %s", err)
		return nil
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		logging.Errorf("Could not connect to Docker: %s", err)
		return nil
	}

	// Start ports at a random number above 10000
	// Check if that port is in use, and if so, increment it.
	// This is to avoid conflicts with other running tests
	const startPort = 10000
	const highestPort = 55534
	bigPort, err := rand.Int(rand.Reader, big.NewInt(highestPort))
	port := uint16(bigPort.Uint64() + startPort)
	if err != nil {
		logging.Errorf("Could not generate random port: %s", err)
		return nil
	}

	for {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			port++
			continue
		}
		listener.Close()
		break
	}

	// pulls an image, creates a container based on it and runs it
	t.redisContainer, err = pool.RunWithOptions(&dockertest.RunOptions{
		Repository: RedisImageName,
		Tag:        RedisTag,
		Cmd:        []string{"--requirepass", "password"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"6379/tcp": {
				{
					HostIP:   "127.0.0.1",
					HostPort: fmt.Sprintf("%d", port),
				},
			},
		},
	})
	if err != nil {
		logging.Errorf("Could not start resource: %s", err)
		return nil
	}

	const connsPerCPU = 10
	const maxIdleTime = 10 * time.Minute

	t.client = redis.NewClient(&redis.Options{
		Addr:            t.redisContainer.GetHostPort("6379/tcp"),
		Password:        "password",
		PoolFIFO:        true,
		PoolSize:        runtime.GOMAXPROCS(0) * connsPerCPU,
		MinIdleConns:    runtime.GOMAXPROCS(0),
		ConnMaxIdleTime: maxIdleTime,
	})

	connected := false
	triesLeft := 10

	for !connected && triesLeft > 0 {
		_, err = t.client.Ping(context.Background()).Result()
		if err != nil {
			triesLeft--
			time.Sleep(1 * time.Second)
		} else {
			connected = true
		}
	}

	if !connected {
		logging.Errorf("Could not connect to redis: %s", err)
		_ = t.client.Close()
		_ = t.redisContainer.Close()
		return nil
	}

	return t.client
}

func (t *TestDB) CloseRedis() {
	if t.client != nil {
		_ = t.client.Close()
	}
	if t.redisContainer != nil {
		_ = t.redisContainer.Close()
	}
	t.redisContainer = nil
	t.client = nil
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

	t.database, err = db.MakeDB(&defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database: %w", err)
	}

	pubsub, err := pubsub.MakePubSub(&defConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	return http.CreateRouter(&defConfig, t.database, pubsub, "test", "deadbeef"), &t, nil
}
