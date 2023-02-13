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

package testutils

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/gin-gonic/gin"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
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
		klog.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		klog.Fatalf("Could not connect to Docker: %s", err)
	}

	// Start ports at a random number above 10000
	// Check if that port is in use, and if so, increment it.
	// This is to avoid conflicts with other running tests
	const highestPort = 55534
	bigPort, err := rand.Int(rand.Reader, big.NewInt(highestPort))
	port := uint16(bigPort.Uint64())
	if err != nil {
		klog.Fatalf("Could not generate random port: %s", err)
	}

	for {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			port++
			continue
		} else {
			listener.Close()
			break
		}
	}

	// pulls an image, creates a container based on it and runs it
	t.redisContainer, err = pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "redis",
		Tag:        "7-alpine",
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
		klog.Fatalf("Could not start resource: %s", err)
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
	_, err = t.client.Ping(context.Background()).Result()
	if err != nil {
		_ = t.redisContainer.Close()
		klog.Fatalf("Failed to connect to redis: %s", err)
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

func CreateTestDBRouter() (*gin.Engine, *TestDB) {
	os.Setenv("TEST", "test")
	var t TestDB
	t.database = db.MakeDB()
	return http.CreateRouter(db.MakeDB(), t.createRedis()), &t
}
