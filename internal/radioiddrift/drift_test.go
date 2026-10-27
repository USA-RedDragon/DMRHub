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

package radioiddrift_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/radioiddrift"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setup(t *testing.T, be testutils.Backend) (*config.Config, *gorm.DB, kv.KV) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_drift_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	be.Migrated().Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = kvStore.Close()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	return &cfg, database, kvStore
}

type directory map[uint]string

func (d directory) lookup(id uint) (string, bool) {
	c, ok := d[id]
	return c, ok
}

func (d directory) size() int {
	return len(d)
}

func drifts(t *testing.T, database *gorm.DB) map[uint]models.RadioIDDrift {
	t.Helper()
	var rows []models.RadioIDDrift
	require.NoError(t, database.Find(&rows).Error)
	out := make(map[uint]models.RadioIDDrift, len(rows))
	for _, r := range rows {
		out[r.UserID] = r
	}
	return out
}

func TestCheckFindsUsersThatRadioIDNoLongerMatches(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, database, kvStore := setup(t, be)
			cfg.DMR.DisableRadioIDValidation = false
			for _, u := range []models.User{
				{ID: 3100001, Callsign: "N0SAME", Username: "same"},
				{ID: 3100002, Callsign: "N0OLD", Username: "moved"},
				{ID: 3100003, Callsign: "N0GONE", Username: "gone"},
				{ID: 3100004, Callsign: "N0ADMIN", Username: "chosen"},
				{ID: 3100005, Callsign: "N0DEL", Username: "deleted"},
			} {
				require.NoError(t, database.Create(&u).Error)
			}
			require.NoError(t, models.SetCallsignOverride(database, 3100004, true))
			require.NoError(t, database.Delete(&models.User{ID: 3100005}).Error)
			dir := directory{3100001: "n0same", 3100002: "N0NEW", 3100004: "N0RID", 3100005: "N0RID2", 9999999: "N0NONE"}

			first := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
			res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), first)
			require.NoError(t, err)
			assert.Equal(t, radioiddrift.Result{Ran: true, Users: 3, Drifts: 2}, res)
			got := drifts(t, database)
			require.Len(t, got, 2)
			assert.Equal(t, "N0NEW", got[3100002].RadioIDCallsign)
			assert.Empty(t, got[3100003].RadioIDCallsign)
			assert.WithinDuration(t, first, got[3100003].FoundAt, time.Second)

			rows, err := models.ListRadioIDDrifts(database)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			assert.Equal(t, "moved", rows[0].Username)
			assert.Equal(t, "N0OLD", rows[0].Callsign)
			n, err := models.CountRadioIDDrifts(database)
			require.NoError(t, err)
			assert.Equal(t, int64(2), n)

			dir[3100002] = "N0OLD"
			second := first.Add(24 * time.Hour)
			require.NoError(t, kvStore.Delete(context.Background(), radioiddrift.LockKey))
			res, err = radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), second)
			require.NoError(t, err)
			assert.Equal(t, radioiddrift.Result{Ran: true, Users: 3, Drifts: 1}, res)
			got = drifts(t, database)
			require.Len(t, got, 1)
			assert.WithinDuration(t, first, got[3100003].FoundAt, time.Second)
			assert.WithinDuration(t, second, got[3100003].CheckedAt, time.Second)
		})
	}
}

func TestCheckRunsOnOneReplica(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = false
	require.NoError(t, database.Create(&models.User{ID: 3100002, Callsign: "N0OLD", Username: "moved"}).Error)
	dir := directory{3100002: "N0NEW"}
	now := time.Now()
	res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), now)
	require.NoError(t, err)
	assert.True(t, res.Ran)
	res, err = radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), now.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, res.Ran)
}

func TestCheckClearsTheListWithoutRadioIDValidation(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = true
	require.NoError(t, database.Create(&models.User{ID: 3100002, Callsign: "N0OLD", Username: "moved"}).Error)
	now := time.Now()
	require.NoError(t, models.ReplaceRadioIDDrifts(database, []models.RadioIDDrift{{UserID: 3100002, RadioIDCallsign: "N0NEW"}}, now))
	dir := directory{3100002: "N0NEW"}
	res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), now)
	require.NoError(t, err)
	assert.False(t, res.Ran)
	assert.Empty(t, drifts(t, database))
}

func TestCheckRefusesAnEmptyRadioIDDatabase(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = false
	require.NoError(t, database.Create(&models.User{ID: 3100002, Callsign: "N0OLD", Username: "moved"}).Error)
	now := time.Now()
	require.NoError(t, models.ReplaceRadioIDDrifts(database, []models.RadioIDDrift{{UserID: 3100002, RadioIDCallsign: "N0NEW"}}, now))
	dir := directory{}
	_, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), now)
	require.Error(t, err)
	assert.Len(t, drifts(t, database), 1)
}

func TestNextRunIsTheNextOneOClock(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("CDT", -5*3600)
	for from, want := range map[time.Time]time.Time{
		time.Date(2026, 10, 27, 0, 30, 0, 0, loc):  time.Date(2026, 10, 27, 1, 0, 0, 0, loc),
		time.Date(2026, 10, 27, 1, 0, 0, 0, loc):   time.Date(2026, 10, 28, 1, 0, 0, 0, loc),
		time.Date(2026, 10, 27, 23, 59, 0, 0, loc): time.Date(2026, 10, 28, 1, 0, 0, 0, loc),
	} {
		assert.Equal(t, want, radioiddrift.NextRun(from), from.String())
	}
}
