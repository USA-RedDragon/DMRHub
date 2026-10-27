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
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
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

// Main runs the tests of a package, as m.Run does, with containers that the
// tests share: one Postgres server, where each test gets a database of its
// own, and MySQL, MariaDB and Redis containers that a test takes over when
// an earlier test lets them go, cleared of its data. It removes the containers when
// the tests end. Without Main, each test starts containers of its own.
func Main(m *testing.M) {
	sharing.Store(true)
	run := func() int {
		go warm()
		return m.Run()
	}
	os.Exit(dockertest.Wrap(context.Background(), run, dockertest.WithTeardown(closeShared)).Run())
}

// warm starts the shared Postgres server and a group of Redis servers while
// the first tests run, so that the tests that need them wait less.
func warm() {
	go func() { _ = ensurePostgres() }()
	if ensurePool() != nil {
		return
	}
	if c, ok, err := startRedisGroup(shared.pool); err == nil && ok {
		shared.redis.give(c)
	}
}

func closeShared(ctx context.Context) error {
	shared.mu.Lock()
	all := shared.all
	shared.all = nil
	shared.mu.Unlock()
	errs := make([]error, len(all))
	var wg sync.WaitGroup
	for i, r := range all {
		wg.Go(func() { errs[i] = r.Close(ctx) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

var sharing atomic.Bool //nolint:gochecknoglobals // set once by Main for the test process

var shared struct { //nolint:gochecknoglobals // the containers that the tests of the process share
	poolOnce sync.Once
	pool     dockertest.Pool
	poolErr  error

	pgOnce  sync.Once
	pgPort  int
	pgAdmin *sql.DB
	pgErr   error

	seq atomic.Int64

	mu  sync.Mutex
	all []dockertest.ClosableResource

	redis spares
}

type spare struct {
	res  dockertest.ClosableResource
	port int
}

type spares struct {
	mu   sync.Mutex
	free []spare
}

func (s *spares) take() (spare, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.free) == 0 {
		return spare{}, false
	}
	c := s.free[len(s.free)-1]
	s.free = s.free[:len(s.free)-1]
	return c, true
}

func (s *spares) give(c spare) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.free = append(s.free, c)
}

func ensurePool() error {
	shared.poolOnce.Do(func() {
		shared.pool, shared.poolErr = dockertest.NewPool(context.Background(), "", dockertest.WithMaxWait(3*time.Minute))
	})
	return shared.poolErr
}

func sharedPool(t *testing.T) dockertest.Pool {
	t.Helper()
	if err := ensurePool(); err != nil {
		t.Skip("Docker not available: " + err.Error())
	}
	return shared.pool
}

func ownPool(t *testing.T, maxWait time.Duration) dockertest.Pool {
	t.Helper()
	pool, err := dockertest.NewPool(t.Context(), "", dockertest.WithMaxWait(maxWait))
	if err != nil {
		t.Skip("Docker not available: " + err.Error())
	}
	t.Cleanup(func() { _ = pool.Close(context.WithoutCancel(t.Context())) })
	return pool
}

func runContainer(ctx context.Context, pool dockertest.Pool, repo, tag string, env, cmd []string) (dockertest.ClosableResource, error) {
	opts := []dockertest.RunOption{
		dockertest.WithTag(tag),
		dockertest.WithoutReuse(),
		dockertest.WithHostConfig(func(hc *container.HostConfig) {
			hc.AutoRemove = true
			hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyDisabled}
			hc.NanoCPUs = 4e9
		}),
	}
	if env != nil {
		opts = append(opts, dockertest.WithEnv(env))
	}
	if cmd != nil {
		opts = append(opts, dockertest.WithCmd(cmd))
	}
	res, err := pool.Run(ctx, repo, opts...)
	if err != nil {
		return nil, fmt.Errorf("start %s container: %w", repo, err)
	}
	if pool == shared.pool {
		shared.mu.Lock()
		shared.all = append(shared.all, res)
		shared.mu.Unlock()
	}
	return res, nil
}

func pingSQL(ctx context.Context, driver, dsn string) error {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("opening %s probe: %w", driver, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	pingErr := db.PingContext(ctx)
	if closeErr := db.Close(); closeErr != nil {
		return fmt.Errorf("closing %s probe: %w", driver, closeErr)
	}
	if pingErr != nil {
		return fmt.Errorf("pinging %s: %w", driver, pingErr)
	}
	return nil
}

func postgresDSN(port int, database string) string {
	return fmt.Sprintf("postgres://test:test@localhost:%d/%s?sslmode=disable", port, database)
}

func mysqlDSN(port int, database string) string {
	return fmt.Sprintf("root:test@tcp(localhost:%d)/%s", port, database)
}

func startPostgres(ctx context.Context, pool dockertest.Pool, cmd []string) (dockertest.ClosableResource, int, error) {
	env := []string{
		"POSTGRES_USER=test",
		"POSTGRES_PASSWORD=test",
		"POSTGRES_DB=testdb",
	}
	if cmd != nil {
		env = append(env, "POSTGRES_INITDB_ARGS=--no-sync")
	}
	res, err := runContainer(ctx, pool, "postgres", "16-alpine", env, cmd)
	if err != nil {
		return nil, 0, err
	}
	port, _ := strconv.Atoi(res.GetPort("5432/tcp"))
	if err := pool.Retry(ctx, 0, func() error { return pingSQL(ctx, "pgx", postgresDSN(port, "testdb")) }); err != nil {
		return nil, 0, fmt.Errorf("postgres not ready: %w", err)
	}
	return res, port, nil
}

func startRedis(ctx context.Context, pool dockertest.Pool) (dockertest.ClosableResource, int, error) {
	res, err := runContainer(ctx, pool, "redis", "7-alpine", nil, nil)
	if err != nil {
		return nil, 0, err
	}
	port, _ := strconv.Atoi(res.GetPort("6379/tcp"))
	if err := pool.Retry(ctx, 0, func() error { return pingRedis(ctx, port) }); err != nil {
		return nil, 0, fmt.Errorf("redis not ready: %w", err)
	}
	return res, port, nil
}

type mysqlFamily struct {
	name, image, tag, envPrefix string
	fast                        []string
	spares                      spares
}

var (
	mysqlServer = &mysqlFamily{ //nolint:gochecknoglobals // the MySQL containers that the tests of the process lend each other
		name: "mysql", image: "mysql", tag: "8.4", envPrefix: "MYSQL",
		fast: []string{"--skip-log-bin", "--innodb-flush-log-at-trx-commit=0", "--innodb-doublewrite=OFF", "--max-connections=4000"},
	}
	mariadbServer = &mysqlFamily{ //nolint:gochecknoglobals // the MariaDB containers that the tests of the process lend each other
		name: "mariadb", image: "mariadb", tag: "12.3", envPrefix: "MARIADB",
		fast: []string{"--innodb-flush-log-at-trx-commit=0", "--max-connections=4000"},
	}
)

func (f *mysqlFamily) start(ctx context.Context, pool dockertest.Pool, fast bool) (dockertest.ClosableResource, int, error) {
	env := []string{
		f.envPrefix + "_ROOT_PASSWORD=test",
		f.envPrefix + "_DATABASE=testdb",
	}
	var cmd []string
	if fast {
		cmd = f.fast
		env = append(env, f.envPrefix+"_INITDB_SKIP_TZINFO=1")
	}
	res, err := runContainer(ctx, pool, f.image, f.tag, env, cmd)
	if err != nil {
		return nil, 0, err
	}
	port, _ := strconv.Atoi(res.GetPort("3306/tcp"))
	if err := pool.Retry(ctx, 0, func() error {
		db, err := gorm.Open(mysql.Open(mysqlDSN(port, "testdb")), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			return fmt.Errorf("opening %s probe: %w", f.name, err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			return fmt.Errorf("opening %s probe: %w", f.name, err)
		}
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		pingErr := sqlDB.PingContext(pctx)
		if closeErr := sqlDB.Close(); closeErr != nil {
			return fmt.Errorf("closing %s probe: %w", f.name, closeErr)
		}
		if pingErr != nil {
			return fmt.Errorf("pinging %s: %w", f.name, pingErr)
		}
		return nil
	}); err != nil {
		return nil, 0, fmt.Errorf("%s not ready: %w", f.name, err)
	}
	return res, port, nil
}

func newDatabaseName() string {
	return fmt.Sprintf("t%d", shared.seq.Add(1))
}

func ensurePostgres() error {
	shared.pgOnce.Do(func() {
		if shared.pgErr = ensurePool(); shared.pgErr != nil {
			return
		}
		var err error
		_, shared.pgPort, err = startPostgres(context.Background(), shared.pool, []string{
			"postgres", "-c", "fsync=off", "-c", "synchronous_commit=off",
			"-c", "full_page_writes=off", "-c", "max_connections=2000",
		})
		if err != nil {
			shared.pgErr = err
			return
		}
		shared.pgAdmin, shared.pgErr = sql.Open("pgx", postgresDSN(shared.pgPort, "postgres"))
	})
	return shared.pgErr
}

func sharedPostgres(t *testing.T) (int, string) {
	t.Helper()
	sharedPool(t)
	if err := ensurePostgres(); err != nil {
		t.Fatalf("shared postgres: %v", err)
	}
	name := newDatabaseName()
	if _, err := shared.pgAdmin.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create postgres database %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = shared.pgAdmin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return shared.pgPort, name
}

func sharedRedis(t *testing.T) int {
	t.Helper()
	pool := sharedPool(t)
	c, ok := shared.redis.take()
	for !ok {
		var err error
		c, ok, err = startRedisGroup(pool)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if err := clearRedis(c.port); err == nil {
			shared.redis.give(c)
		}
	})
	return c.port
}

const (
	redisGroupSize = 16
	redisGroupBase = 7000
)

var redisStart sync.Mutex //nolint:gochecknoglobals // guards the start of shared Redis groups

// startRedisGroup starts a container that runs redisGroupSize Redis servers,
// each on a port of its own, and adds all but one to the spares. When
// another test started a group meanwhile, it takes a spare of that group
// instead.
func startRedisGroup(pool dockertest.Pool) (spare, bool, error) {
	redisStart.Lock()
	defer redisStart.Unlock()
	if c, ok := shared.redis.take(); ok {
		return c, true, nil
	}
	ctx := context.Background()
	exposed := network.PortSet{}
	script := ""
	for i := range redisGroupSize {
		port := redisGroupBase + i
		exposed[network.MustParsePort(fmt.Sprintf("%d/tcp", port))] = struct{}{}
		if i < redisGroupSize-1 {
			script += fmt.Sprintf("redis-server --port %d --protected-mode no --save '' --appendonly no --daemonize yes && ", port)
		} else {
			script += fmt.Sprintf("exec redis-server --port %d --protected-mode no --save '' --appendonly no", port)
		}
	}
	res, err := pool.Run(ctx, "redis",
		dockertest.WithTag("7-alpine"),
		dockertest.WithoutReuse(),
		dockertest.WithEntrypoint([]string{"sh", "-c"}),
		dockertest.WithCmd([]string{script}),
		dockertest.WithContainerConfig(func(c *container.Config) { c.ExposedPorts = exposed }),
		dockertest.WithHostConfig(func(hc *container.HostConfig) {
			hc.AutoRemove = true
			hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyDisabled}
			hc.NanoCPUs = 4e9
		}),
	)
	if err != nil {
		return spare{}, false, fmt.Errorf("start redis container: %w", err)
	}
	shared.mu.Lock()
	shared.all = append(shared.all, res)
	shared.mu.Unlock()
	var group []spare
	for i := range redisGroupSize {
		port, _ := strconv.Atoi(res.GetPort(fmt.Sprintf("%d/tcp", redisGroupBase+i)))
		if err := pool.Retry(ctx, 0, func() error { return pingRedis(ctx, port) }); err != nil {
			return spare{}, false, fmt.Errorf("redis not ready: %w", err)
		}
		group = append(group, spare{res: res, port: port})
	}
	for _, c := range group[1:] {
		shared.redis.give(c)
	}
	return group[0], true, nil
}

func pingRedis(ctx context.Context, port int) error {
	rdb := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("localhost:%d", port)})
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	pingErr := rdb.Ping(ctx).Err()
	if closeErr := rdb.Close(); closeErr != nil {
		return fmt.Errorf("closing redis probe: %w", closeErr)
	}
	if pingErr != nil {
		return fmt.Errorf("pinging redis: %w", pingErr)
	}
	return nil
}

func clearRedis(port int) error {
	rdb := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("localhost:%d", port)})
	defer func() { _ = rdb.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, typ := range []string{"pubsub", "normal"} {
		if err := rdb.Do(ctx, "CLIENT", "KILL", "TYPE", typ, "SKIPME", "yes").Err(); err != nil {
			return fmt.Errorf("killing redis clients: %w", err)
		}
	}
	if err := rdb.FlushAll(ctx).Err(); err != nil {
		return fmt.Errorf("flushing redis: %w", err)
	}
	return nil
}

func (f *mysqlFamily) shared(t *testing.T) (int, string) {
	t.Helper()
	pool := sharedPool(t)
	c, ok := f.spares.take()
	if !ok {
		res, port, err := f.start(context.Background(), pool, true)
		if err != nil {
			t.Fatal(err)
		}
		c = spare{res: res, port: port}
	}
	admin, err := sql.Open("mysql", mysqlDSN(c.port, ""))
	if err != nil {
		t.Fatalf("open %s admin: %v", f.name, err)
	}
	admin.SetMaxOpenConns(1)
	name := newDatabaseName()
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		_ = admin.Close()
		t.Fatalf("create %s database %s: %v", f.name, name, err)
	}
	t.Cleanup(func() {
		err := clearMySQL(admin, name)
		_ = admin.Close()
		if err != nil {
			_ = c.res.Close(context.Background())
			return
		}
		f.spares.give(c)
	})
	return c.port, name
}

func clearMySQL(admin *sql.DB, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := admin.QueryContext(ctx, "SELECT id FROM information_schema.processlist WHERE user = 'root' AND id <> CONNECTION_ID()")
	if err != nil {
		return fmt.Errorf("listing mysql sessions: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("listing mysql sessions: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("listing mysql sessions: %w", err)
	}
	for _, id := range ids {
		_, _ = admin.ExecContext(ctx, fmt.Sprintf("KILL %d", id))
	}
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+name); err != nil {
		return fmt.Errorf("dropping mysql database %s: %w", name, err)
	}
	return nil
}

// MySQLBackend returns a backend with a MySQL database of its own and the
// in-memory pubsub and KV. The root password is "test", so that a test can
// make more databases.
func MySQLBackend() Backend {
	return mysqlFamilyBackend(mysqlServer)
}

// MariaDBBackend returns a backend with a database of its own on a MariaDB
// server of the latest long-term release and the in-memory pubsub and KV.
// The root password is "test", so that a test can make more databases.
func MariaDBBackend() Backend {
	return mysqlFamilyBackend(mariadbServer)
}

func mysqlFamilyBackend(f *mysqlFamily) Backend {
	return Backend{
		Name: f.name,
		Setup: func(t *testing.T, cfg *config.Config) {
			t.Helper()
			var port int
			database := "testdb"
			if sharing.Load() {
				port, database = f.shared(t)
			} else {
				var err error
				_, port, err = f.start(t.Context(), ownPool(t, 3*time.Minute), false)
				if err != nil {
					t.Fatal(err)
				}
			}
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

// PostgresRedisBackend returns a backend with a Postgres database and a
// Redis server of its own.
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
// of its containers in c when c is not nil. A backend with c not nil always
// starts containers of its own, since the test may stop them.
func PostgresRedisBackendInto(c *Containers) Backend {
	return postgresRedis(c, c != nil, c != nil)
}

// PostgresOwnRedisBackendInto is PostgresRedisBackend with a Redis container
// of its own, whose ID it stores in c.Redis, for a test that stops or
// pauses Redis and leaves Postgres alone.
func PostgresOwnRedisBackendInto(c *Containers) Backend {
	return postgresRedis(c, false, true)
}

func postgresRedis(c *Containers, ownPostgres, ownRedis bool) Backend {
	return Backend{
		Name: "postgres-redis",
		Setup: func(t *testing.T, cfg *config.Config) {
			t.Helper()
			var pgPort, redisPort int
			database := "testdb"
			var pool dockertest.Pool
			if !sharing.Load() || ownPostgres || ownRedis {
				pool = ownPool(t, 60*time.Second)
			}
			if sharing.Load() && !ownPostgres {
				pgPort, database = sharedPostgres(t)
			} else {
				res, port, err := startPostgres(t.Context(), pool, nil)
				if err != nil {
					t.Fatal(err)
				}
				pgPort = port
				if c != nil {
					c.Postgres = res.ID()
				}
			}
			if sharing.Load() && !ownRedis {
				redisPort = sharedRedis(t)
			} else {
				res, port, err := startRedis(t.Context(), pool)
				if err != nil {
					t.Fatal(err)
				}
				redisPort = port
				if c != nil {
					c.Redis = res.ID()
				}
			}

			cfg.Database.Driver = config.DatabaseDriverPostgres
			cfg.Database.Host = "localhost"
			cfg.Database.Port = pgPort
			cfg.Database.Username = "test"
			cfg.Database.Password = "test"
			cfg.Database.Database = database
			cfg.Database.ExtraParameters = []string{"sslmode=disable"}

			cfg.Redis.Enabled = true
			cfg.Redis.Host = "localhost"
			cfg.Redis.Port = redisPort
		},
	}
}
