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
package db_test

import (
	"fmt"
	"net"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func serverBackends() []testutils.Backend {
	return []testutils.Backend{testutils.MySQLBackend(), testutils.MariaDBBackend(), testutils.PostgresRedisBackend()}
}

func tablesOf(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	gdb, err := gorm.Open(db.Dialect(cfg), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	defer closeDB(t, gdb)
	query := "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()"
	if cfg.Database.Driver == config.DatabaseDriverMySQL {
		query = "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE()"
	}
	tables := []string{}
	require.NoError(t, gdb.Raw(query).Scan(&tables).Error)
	return tables
}

func closedPort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr, ok := l.Addr().(*net.TCPAddr)
	require.True(t, ok)
	require.NoError(t, l.Close())
	return addr.Port
}

func pingWithin(t *testing.T, cfg *config.Config) error {
	t.Helper()
	if err := db.Ping(t.Context(), cfg); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// TestPingReachesEachServerAndMakesNothing pings an empty database on
// MySQL, MariaDB and Postgres. Ping reaches it and leaves it empty: no
// table, so no row.
func TestPingReachesEachServerAndMakesNothing(t *testing.T) {
	t.Parallel()
	for _, be := range serverBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg := backendConfig(t, be)
			require.Empty(t, tablesOf(t, &cfg), "the database of the test starts empty")
			require.NoError(t, pingWithin(t, &cfg))
			assert.Empty(t, tablesOf(t, &cfg), "Ping made tables")
		})
	}
}

// TestPingRefusesAWrongPassword pings MySQL, MariaDB and Postgres with a
// password that the server does not take.
func TestPingRefusesAWrongPassword(t *testing.T) {
	t.Parallel()
	for _, be := range serverBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg := backendConfig(t, be)
			cfg.Database.Password = "not-the-password"
			require.Error(t, pingWithin(t, &cfg))
		})
	}
}

// TestPingRefusesAServerThatIsNotThere pings MySQL, MariaDB and Postgres
// settings with a port where nothing listens, and with a host that does
// not exist.
func TestPingRefusesAServerThatIsNotThere(t *testing.T) {
	t.Parallel()
	for _, be := range serverBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg := backendConfig(t, be)
			port := cfg
			port.Database.Host = "127.0.0.1"
			port.Database.Port = closedPort(t)
			require.Error(t, pingWithin(t, &port), "a port where nothing listens")
			host := cfg
			host.Database.Host = "no-such-host.invalid"
			require.Error(t, pingWithin(t, &host), "a host that does not exist")
		})
	}
}
