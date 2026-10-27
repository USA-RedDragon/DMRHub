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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func builtinsDB(t *testing.T, be testutils.Backend) (*gorm.DB, *config.Config) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	be.Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return database, &cfg
}

func TestRemovingTheBuiltinServicesDropsTheParrotSeeds(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			database, cfg := builtinsDB(t, be)
			require.NoError(t, database.Create(&models.User{ID: 9990, Callsign: "Parrot", Approved: true}).Error)
			require.NoError(t, database.Create(&models.Talkgroup{ID: 9990, Name: "DMRHub Parrot", Description: "This talkgroup will not be routed to any repeaters and Parrot will respond with a private call."}).Error)
			require.NoError(t, database.Create(&models.User{ID: 3191868, Callsign: "KI5VMF", Username: "owner", Approved: true}).Error)
			tg := uint(9990)
			rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 319186801, Callsign: "KI5VMF"}, OwnerID: 3191868,
				Type: models.RepeaterTypeMMDVM, TS2DynamicTalkgroupID: &tg}
			require.NoError(t, database.Create(&rpt).Error)
			require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 9990}))
			from, to := uint(9990), uint(3191868)
			require.NoError(t, database.Create(&models.Call{StreamID: 1, StartTime: time.Now(), UserID: &from, SourceID: 9990, DestinationID: 3191868, IsToUser: true, ToUserID: &to}).Error)
			require.NoError(t, database.Create(&models.Call{StreamID: 2, StartTime: time.Now(), UserID: &to, SourceID: 3191868, DestinationID: 9990, IsToTalkgroup: true, ToTalkgroupID: &tg}).Error)

			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610240200").Error)
			require.NoError(t, migration.Migrate(database, cfg))

			var n int64
			require.NoError(t, database.Unscoped().Model(&models.User{}).Where("id = ?", 9990).Count(&n).Error)
			assert.Zero(t, n, "the parrot user")
			require.NoError(t, database.Unscoped().Model(&models.Talkgroup{}).Where("id = ?", 9990).Count(&n).Error)
			assert.Zero(t, n, "the parrot talkgroup")
			require.NoError(t, database.Table("repeater_ts1_static_talkgroups").Where("talkgroup_id = ?", 9990).Count(&n).Error)
			assert.Zero(t, n)
			var got models.Repeater
			require.NoError(t, database.First(&got, 319186801).Error)
			assert.Nil(t, got.TS2DynamicTalkgroupID)
			var calls []models.Call
			require.NoError(t, database.Order("stream_id").Find(&calls).Error)
			require.Len(t, calls, 2, "the calls stay")
			assert.Nil(t, calls[0].UserID)
			assert.Equal(t, uint(9990), calls[0].SourceID)
			assert.Nil(t, calls[1].ToTalkgroupID)
			assert.False(t, calls[1].IsToTalkgroup)
			assert.False(t, database.Migrator().HasTable("location_requests"))
		})
	}
}

func TestRemovingTheBuiltinServicesKeepsAnAdminsTalkgroup9990(t *testing.T) {
	t.Parallel()
	database, cfg := builtinsDB(t, testutils.SQLiteMemoryBackend())
	require.NoError(t, database.Create(&models.Talkgroup{ID: 9990, Name: "Echo net"}).Error)
	require.NoError(t, database.Create(&models.User{ID: 9990, Callsign: "N0ECHO", Username: "echo", Approved: true}).Error)
	require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610240200").Error)
	require.NoError(t, migration.Migrate(database, cfg))
	var n int64
	require.NoError(t, database.Model(&models.Talkgroup{}).Where("id = ?", 9990).Count(&n).Error)
	assert.Equal(t, int64(1), n)
	require.NoError(t, database.Model(&models.User{}).Where("id = ?", 9990).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}
