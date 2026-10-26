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
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/moby/moby/api/types/container"
	"github.com/ory/dockertest/v4"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Backend describes a database/cache backend configuration for integration tests.
type Backend struct {
	Name  string
	Setup func(t *testing.T, cfg *config.Config)
	// WrapKV and WrapPubSub, when not nil, wrap the KV store and the
	// pubsub of an integration stack, such as to slow them down.
	WrapKV     func(kv.KV) kv.KV
	WrapPubSub func(pubsub.PubSub) pubsub.PubSub
}

// SQLiteMemoryBackend returns a backend using in-memory SQLite and in-memory pubsub/KV.
func SQLiteMemoryBackend() Backend {
	return Backend{
		Name:  "sqlite-memory",
		Setup: func(_ *testing.T, _ *config.Config) {},
	}
}

// MySQLBackend returns a backend with a database of its own for each test
// on a MySQL server in Docker, with the in-memory pubsub and KV. The tests
// of a process share the server while any of them runs. The root password
// is "test", so that a test can make more databases.
func MySQLBackend() Backend {
	return mysqlFamilyBackend(mysqlServer)
}

// MariaDBBackend returns a backend with a database of its own for each
// test on a MariaDB server of the latest long-term release in Docker, with
// the in-memory pubsub and KV. The tests of a process share the server
// while any of them runs. The root password is "test", so that a test can
// make more databases.
func MariaDBBackend() Backend {
	return mysqlFamilyBackend(mariadbServer)
}

type sharedServer struct {
	name, image, tag, envPrefix string
	cmd                         []string

	mu    sync.Mutex
	users int
	seq   int
	port  int
	pool  dockertest.ClosablePool
	root  *gorm.DB
}

var (
	mysqlServer = &sharedServer{ //nolint:gochecknoglobals
		name: "mysql", image: "mysql", tag: "8.4", envPrefix: "MYSQL",
		cmd: []string{"--max-connections=4000", "--innodb-flush-log-at-trx-commit=0", "--skip-log-bin"},
	}
	mariadbServer = &sharedServer{ //nolint:gochecknoglobals
		name: "mariadb", image: "mariadb", tag: "12.3", envPrefix: "MARIADB",
		cmd: []string{"--max-connections=4000", "--innodb-flush-log-at-trx-commit=0"},
	}
)

func (s *sharedServer) start(t *testing.T) {
	t.Helper()
	pool, err := dockertest.NewPool(context.Background(), "", dockertest.WithMaxWait(3*time.Minute))
	if err != nil {
		t.Skip("Docker not available: " + err.Error())
	}
	res, err := pool.Run(context.Background(), s.image,
		dockertest.WithTag(s.tag),
		dockertest.WithEnv([]string{s.envPrefix + "_ROOT_PASSWORD=test"}),
		dockertest.WithCmd(s.cmd),
		dockertest.WithoutReuse(),
		dockertest.WithHostConfig(func(hc *container.HostConfig) {
			hc.AutoRemove = true
			hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyDisabled}
			hc.NanoCPUs = 4e9
		}),
	)
	if err != nil {
		_ = pool.Close(context.Background())
		t.Fatalf("start %s container: %v", s.name, err)
	}
	port, _ := strconv.Atoi(res.GetPort("3306/tcp"))
	var root *gorm.DB
	if err := pool.Retry(context.Background(), 0, func() error {
		db, err := gorm.Open(mysql.Open(fmt.Sprintf("root:test@tcp(localhost:%d)/", port)), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			return fmt.Errorf("opening %s probe: %w", s.name, err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			return fmt.Errorf("opening %s probe: %w", s.name, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(ctx); err != nil {
			_ = sqlDB.Close()
			return fmt.Errorf("pinging %s: %w", s.name, err)
		}
		root = db
		return nil
	}); err != nil {
		_ = pool.Close(context.Background())
		t.Fatalf("%s not ready: %v", s.name, err)
	}
	s.pool, s.port, s.root = pool, port, root
}

func (s *sharedServer) stop() {
	if sqlDB, err := s.root.DB(); err == nil {
		_ = sqlDB.Close()
	}
	_ = s.pool.Close(context.Background())
	s.pool, s.root = nil, nil
}

// acquire makes a new database on the server, starting the server when no
// test is using it.
func (s *sharedServer) acquire(t *testing.T) (port int, database string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == 0 {
		s.start(t)
	}
	s.users++
	s.seq++
	database = fmt.Sprintf("test%d", s.seq)
	root := s.root
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = root.WithContext(ctx).Exec("DROP DATABASE IF EXISTS " + database).Error
		cancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.users--
		if s.users == 0 {
			s.stop()
		}
	})
	if err := root.Exec("CREATE DATABASE " + database).Error; err != nil {
		t.Fatalf("create %s database: %v", s.name, err)
	}
	return s.port, database
}

func mysqlFamilyBackend(s *sharedServer) Backend {
	return Backend{
		Name: s.name,
		Setup: func(t *testing.T, cfg *config.Config) {
			t.Helper()
			port, database := s.acquire(t)
			cfg.Database.Driver = config.DatabaseDriverMySQL
			cfg.Database.Host = "localhost"
			cfg.Database.Port = port
			cfg.Database.Username = "root"
			cfg.Database.Password = "test"
			cfg.Database.Database = database
			cfg.Database.ExtraParameters = nil
		},
	}
}

// PostgresRedisBackend returns a backend that spins up a dedicated Postgres
// and Redis container for each test. Containers are fully parallel.
func PostgresRedisBackend() Backend {
	return PostgresRedisBackendInto(nil)
}

// Containers holds the IDs of the Docker containers of a Postgres and
// Redis backend, so that a test can pause, stop or restart them.
type Containers struct {
	Postgres string
	Redis    string
}

// PostgresRedisBackendInto is PostgresRedisBackend that also stores the IDs
// of its containers in c when c is not nil.
func PostgresRedisBackendInto(c *Containers) Backend {
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
					hc.NanoCPUs = 4e9
				}),
			)
			if err != nil {
				t.Fatalf("start postgres container: %v", err)
			}

			pgPort, _ := strconv.Atoi(pgResource.GetPort("5432/tcp"))
			if c != nil {
				c.Postgres = pgResource.ID()
			}

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
					hc.NanoCPUs = 4e9
				}),
			)
			if err != nil {
				t.Fatalf("start redis container: %v", err)
			}

			redisPort, _ := strconv.Atoi(redisResource.GetPort("6379/tcp"))
			if c != nil {
				c.Redis = redisResource.ID()
			}

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
