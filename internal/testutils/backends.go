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
	"github.com/moby/moby/api/types/container"
	"github.com/ory/dockertest/v4"
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

			pool, poolErr := dockertest.NewPool(t.Context(), "", dockertest.WithMaxWait(60*time.Second))
			if poolErr != nil {
				t.Skip("Docker not available: " + poolErr.Error())
			}
			t.Cleanup(func() { _ = pool.Close(context.WithoutCancel(t.Context())) })

			// --- PostgreSQL ---
			pgResource, err := pool.Run(t.Context(), "postgres",
				dockertest.WithTag("16-alpine"),
				dockertest.WithEnv([]string{
					"POSTGRES_USER=test",
					"POSTGRES_PASSWORD=test",
					"POSTGRES_DB=testdb",
				}),
				dockertest.WithoutReuse(),
				dockertest.WithHostConfig(func(hc *container.HostConfig) {
					hc.AutoRemove = true
					hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyDisabled}
				}),
			)
			if err != nil {
				t.Fatalf("start postgres container: %v", err)
			}

			pgPort, _ := strconv.Atoi(pgResource.GetPort("5432/tcp"))

			if err := pool.Retry(t.Context(), 0, func() error {
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
			redisResource, err := pool.Run(t.Context(), "redis",
				dockertest.WithTag("7-alpine"),
				dockertest.WithoutReuse(),
				dockertest.WithHostConfig(func(hc *container.HostConfig) {
					hc.AutoRemove = true
					hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyDisabled}
				}),
			)
			if err != nil {
				t.Fatalf("start redis container: %v", err)
			}

			redisPort, _ := strconv.Atoi(redisResource.GetPort("6379/tcp"))

			if err := pool.Retry(t.Context(), 0, func() error {
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
