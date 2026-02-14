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
	"database/sql"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"
)

// Backend describes a database/cache backend configuration for integration tests.
type Backend struct {
	Name  string
	Setup func(t *testing.T, cfg *config.Config)
}

// SQLiteMemoryBackend returns a backend using in-memory SQLite and in-memory pubsub/KV.
func SQLiteMemoryBackend() Backend {
	return Backend{
		Name:  "sqlite-memory",
		Setup: func(_ *testing.T, _ *config.Config) {},
	}
}

// PostgresRedisBackend returns a backend that spins up a dedicated Postgres
// and Redis container for each test. Containers are fully parallel.
func PostgresRedisBackend() Backend {
	return Backend{
		Name: "postgres-redis",
		Setup: func(t *testing.T, cfg *config.Config) {
			t.Helper()

			pool, poolErr := dockertest.NewPool("")
			if poolErr != nil {
				t.Skip("Docker not available: " + poolErr.Error())
			}
			pool.MaxWait = 60 * time.Second

			// --- PostgreSQL ---
			pgResource, err := pool.RunWithOptions(&dockertest.RunOptions{
				Repository: "postgres",
				Tag:        "16-alpine",
				Env: []string{
					"POSTGRES_USER=test",
					"POSTGRES_PASSWORD=test",
					"POSTGRES_DB=testdb",
				},
			}, func(hc *docker.HostConfig) {
				hc.AutoRemove = true
				hc.RestartPolicy = docker.RestartPolicy{Name: "no"}
			})
			if err != nil {
				t.Fatalf("start postgres container: %v", err)
			}
			if err := pgResource.Expire(120); err != nil {
				t.Fatalf("setting postgres container expiry: %v", err)
			}
			t.Cleanup(func() { _ = pool.Purge(pgResource) })

			pgPort, _ := strconv.Atoi(pgResource.GetPort("5432/tcp"))

			if err := pool.Retry(func() error {
				db, err := sql.Open("pgx",
					fmt.Sprintf("postgres://test:test@localhost:%d/testdb?sslmode=disable", pgPort))
				if err != nil {
					return fmt.Errorf("opening postgres probe: %w", err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				pingErr := db.PingContext(ctx)
				if closeErr := db.Close(); closeErr != nil {
					return fmt.Errorf("closing postgres probe: %w", closeErr)
				}
				if pingErr != nil {
					return fmt.Errorf("pinging postgres: %w", pingErr)
				}
				return nil
			}); err != nil {
				t.Fatalf("postgres not ready: %v", err)
			}

			// --- Redis ---
			redisResource, err := pool.RunWithOptions(&dockertest.RunOptions{
				Repository: "redis",
				Tag:        "7-alpine",
			}, func(hc *docker.HostConfig) {
				hc.AutoRemove = true
				hc.RestartPolicy = docker.RestartPolicy{Name: "no"}
			})
			if err != nil {
				t.Fatalf("start redis container: %v", err)
			}
			if err := redisResource.Expire(120); err != nil {
				t.Fatalf("setting redis container expiry: %v", err)
			}
			t.Cleanup(func() { _ = pool.Purge(redisResource) })

			redisPort, _ := strconv.Atoi(redisResource.GetPort("6379/tcp"))

			if err := pool.Retry(func() error {
				rdb := redis.NewClient(&redis.Options{
					Addr: fmt.Sprintf("localhost:%d", redisPort),
				})
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				pingErr := rdb.Ping(ctx).Err()
				if closeErr := rdb.Close(); closeErr != nil {
					return fmt.Errorf("closing redis probe: %w", closeErr)
				}
				if pingErr != nil {
					return fmt.Errorf("pinging redis: %w", pingErr)
				}
				return nil
			}); err != nil {
				t.Fatalf("redis not ready: %v", err)
			}

			// Postgres
			cfg.Database.Driver = config.DatabaseDriverPostgres
			cfg.Database.Host = "localhost"
			cfg.Database.Port = pgPort
			cfg.Database.Username = "test"
			cfg.Database.Password = "test"
			cfg.Database.Database = "testdb"
			cfg.Database.ExtraParameters = []string{"sslmode=disable"}

			// Redis
			cfg.Redis.Enabled = true
			cfg.Redis.Host = "localhost"
			cfg.Redis.Port = redisPort
		},
	}
}
