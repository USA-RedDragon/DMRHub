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

package migration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Write(p)
	return len(p), nil
}

func (b *syncBuffer) clashes(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if rec["msg"] == "A username is the callsign of another user. Rename one of them" {
			out = append(out, rec)
		}
	}
	b.buf.Reset()
	return out
}

// TestUsernameCallsignClashesAreLogged makes a database of before the
// check where usernames are the callsigns of other users. At the next
// start DMRHub logs each clash once at WARN with both user IDs and
// changes no data. It does not log them again at later starts.
//
// The test is not parallel, as it replaces the default logger.
func TestUsernameCallsignClashesAreLogged(t *testing.T) { //nolint:paralleltest
	logs := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} { //nolint:paralleltest
		t.Run(be.Name, func(t *testing.T) {
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			cfg.Database.Database = fmt.Sprintf("file:memdb_%p", t)
			cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
			be.Setup(t, &cfg)
			database, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})
			assert.Empty(t, logs.clashes(t), "a new database has no clash")

			users := []*models.User{
				{ID: 3000001, Callsign: "N0CALL", Username: "n1Call", Password: "hash"},
				{ID: 3000002, Callsign: "N1CALL", Username: "bob", Password: "hash"},
				{ID: 3000003, Callsign: "N3CALL", Username: "n3call", Password: "hash"},
				{ID: 3000004, Callsign: "N4CALL", Username: "N0CALL", Password: "hash"},
			}
			for _, u := range users {
				require.NoError(t, database.Create(u).Error)
			}
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610271200").Error)

			require.NoError(t, migration.MigrateSchema(database, &cfg))
			got := logs.clashes(t)
			require.Len(t, got, 2)
			for i, want := range []struct {
				username, callsign     string
				userID, callsignUserID float64
			}{
				{"n1Call", "N1CALL", 3000001, 3000002},
				{"N0CALL", "N0CALL", 3000004, 3000001},
			} {
				assert.Equal(t, "WARN", got[i]["level"])
				assert.Equal(t, want.username, got[i]["username"])
				assert.Equal(t, want.userID, got[i]["userID"])
				assert.Equal(t, want.callsign, got[i]["callsign"])
				assert.Equal(t, want.callsignUserID, got[i]["callsignUserID"])
			}

			var after []models.User
			require.NoError(t, database.Order("id").Find(&after).Error)
			require.Len(t, after, len(users))
			for i, u := range after {
				assert.Equal(t, users[i].Username, u.Username, "the check changes no data")
				assert.Equal(t, users[i].Callsign, u.Callsign, "the check changes no data")
			}

			require.NoError(t, migration.MigrateSchema(database, &cfg))
			again, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			if sqlDB, err := again.DB(); err == nil {
				_ = sqlDB.Close()
			}
			assert.Empty(t, logs.clashes(t), "each clash is logged once")
		})
	}
}
