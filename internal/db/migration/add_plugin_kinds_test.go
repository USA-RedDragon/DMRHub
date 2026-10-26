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
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddPluginKindsKeepsThePluginsExternal migrates a database from before
// plugins ran inside DMRHub. Its plugins must come out as plugins that run
// as their own programs.
func TestAddPluginKindsKeepsThePluginsExternal(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
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

			m := database.Migrator()
			require.NoError(t, m.DropTable(&models.PluginHost{}, &models.PluginModule{}))
			for _, c := range []string{"Kind", "Builtin", "Version", "Author", "Description", "ModuleHash"} {
				require.NoError(t, m.DropColumn(&models.Plugin{}, c))
			}
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610251100").Error)
			require.NoError(t, database.Exec(
				"INSERT INTO plugins (name, token_hash, token_created_at, enabled, session) VALUES (?, ?, ?, ?, ?)",
				"Weather", []byte("0123456789abcdef0123456789abcdef"), time.Now().UTC(), true, "",
			).Error)

			require.NoError(t, migration.Migrate(database, &cfg))

			assert.True(t, m.HasTable(&models.PluginHost{}))
			assert.True(t, m.HasTable(&models.PluginModule{}))
			list, err := models.ListPlugins(database)
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, models.PluginExternal, list[0].Kind)
			assert.Empty(t, list[0].Version)

			p, err := models.CreateUploadedPlugin(database, models.PluginInfo{Name: "Net check-in", Version: "0.4.1",
				Hosts: []models.PluginHost{{Address: "nets.example.org:443"}}}, nil, nil, []byte("wasm"), time.Now())
			require.NoError(t, err)
			_, wasm, err := models.PluginModuleOf(database, p.ID)
			require.NoError(t, err)
			assert.Equal(t, []byte("wasm"), wasm)
		})
	}
}
