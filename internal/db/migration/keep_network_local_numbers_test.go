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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// localTalkgroupsAfter runs the migration again on a database with the
// talkgroups tgs and the Setup settings saved, or none, and returns the
// local talkgroups that DMRHub then runs with, and whether Setup holds
// them.
func localTalkgroupsAfter(t *testing.T, be testutils.Backend, tgs []uint, saved string) ([]uint, bool) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	be.Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	for _, tg := range tgs {
		require.NoError(t, database.Create(&models.Talkgroup{ID: tg, Name: "Network"}).Error)
	}
	require.NoError(t, database.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.SetupSettings{}).Error)
	if saved != "" {
		require.NoError(t, database.Create(&models.SetupSettings{ID: models.SetupSettingsID, Revision: 3, Settings: saved}).Error)
	}
	require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202611020110").Error)
	require.NoError(t, migration.Migrate(database, &cfg))

	values, err := liveconfig.LoadSaved(t.Context(), database)
	require.NoError(t, err)
	got := cfg
	for _, k := range liveconfig.Keys() {
		if raw, ok := values.Values[k.Path]; ok {
			require.NoError(t, k.Set(&got, raw))
		}
	}
	_, held := values.Values["dmr.routing.local-talkgroups"]
	return got.DMR.Routing.LocalTalkgroups, held
}

func TestANetworkTalkgroupOnALocalNumberStaysANetworkTalkgroup(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()

			got, held := localTalkgroupsAfter(t, be, []uint{8, 91}, `{"dmr.routing.contention":"first"}`)
			assert.Equal(t, []uint{9}, got, "TG 8 of the network stays a network talkgroup")
			assert.True(t, held)

			got, _ = localTalkgroupsAfter(t, be, []uint{8, 9}, "")
			assert.Empty(t, got, "with TG 8 and 9 on the network, no number is local")

			got, held = localTalkgroupsAfter(t, be, []uint{91}, "")
			assert.Equal(t, []uint{8, 9}, got, "a network with no TG 8 or 9 takes the defaults")
			assert.False(t, held)

			got, _ = localTalkgroupsAfter(t, be, []uint{9}, `{"dmr.routing.local-talkgroups":[9]}`)
			assert.Equal(t, []uint{9}, got, "the local talkgroups that admins saved stay")
		})
	}
}

func TestKeepNetworkLocalNumbersRollsBack(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, database.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.SetupSettings{}).Error)
	require.NoError(t, database.Create(&models.SetupSettings{ID: models.SetupSettingsID, Settings: `{"dmr.routing.local-talkgroups":[9],"dmr.routing.contention":"first"}`}).Error)
	require.NoError(t, migration.ByID("202611020110").Rollback(database))
	values, err := liveconfig.LoadSaved(t.Context(), database)
	require.NoError(t, err)
	assert.NotContains(t, values.Values, "dmr.routing.local-talkgroups")
	assert.Contains(t, values.Values, "dmr.routing.contention")
}
