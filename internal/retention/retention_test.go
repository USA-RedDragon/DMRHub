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

package retention_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/retention"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const daySpan = 24 * time.Hour

func setup(t *testing.T, be testutils.Backend) (*config.Config, *gorm.DB, kv.KV) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_ret_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	be.Setup(t, &cfg)
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

func seed(t *testing.T, database *gorm.DB, now time.Time, ages ...int) {
	t.Helper()
	for i, age := range ages {
		at := now.Add(-time.Duration(age) * daySpan)
		call := models.Call{StartTime: at, Kind: models.CallKindData, SourceID: uint(i + 1), CallData: []byte{1, 2, 3}}
		require.NoError(t, database.Create(&call).Error)
		require.NoError(t, database.Create(&models.Message{ReceivedAt: at, TransmissionID: &call.ID, SourceID: uint(i + 1), DestinationID: 9, Format: models.MessageFormatIP}).Error)
	}
}

func count(t *testing.T, database *gorm.DB, model any, where ...any) int64 {
	t.Helper()
	var n int64
	q := database.Unscoped().Model(model)
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	require.NoError(t, q.Count(&n).Error)
	return n
}

func TestRetentionPurgesOldRecords(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, database, kvStore := setup(t, be)
			now := time.Now()
			seed(t, database, now, 1, 20, 40, 100, 400)
			cfg.DMR.Retention = config.Retention{Messages: 30, Transmissions: 365, Bursts: 10}

			res, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
			require.NoError(t, err)
			assert.True(t, res.Ran)
			assert.Equal(t, retention.Result{Ran: true, Messages: 3, Transmissions: 1, Bursts: 4}, res)

			assert.Equal(t, int64(2), count(t, database, &models.Message{}))
			assert.Equal(t, int64(0), count(t, database, &models.Message{}, "received_at < ?", now.Add(-30*daySpan)))
			assert.Equal(t, int64(4), count(t, database, &models.Call{}))
			assert.Equal(t, int64(1), count(t, database, &models.Call{}, "call_data IS NOT NULL"))
		})
	}
}

func TestRetentionKeepsMessagesOfPurgedTransmissions(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	now := time.Now()
	seed(t, database, now, 50)
	cfg.DMR.Retention = config.Retention{Transmissions: 30}
	_, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count(t, database, &models.Call{}))
	var m models.Message
	require.NoError(t, database.First(&m).Error)
	assert.Nil(t, m.TransmissionID)
}

func TestRetentionZeroKeepsEverything(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	now := time.Now()
	seed(t, database, now, 1, 4000, 20000)
	res, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
	require.NoError(t, err)
	assert.False(t, res.Ran)
	assert.Equal(t, int64(3), count(t, database, &models.Message{}))
	assert.Equal(t, int64(3), count(t, database, &models.Call{}, "call_data IS NOT NULL"))
}

func TestRetentionRunsOnOneReplica(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	now := time.Now()
	cfg.DMR.Retention = config.Retention{Messages: 30}
	first, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
	require.NoError(t, err)
	second, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
	require.NoError(t, err)
	assert.True(t, first.Ran)
	assert.False(t, second.Ran)
}

func TestRetentionPurgesInBatches(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	now := time.Now()
	old := now.Add(-90 * daySpan)
	rows := make([]models.Message, 2500)
	for i := range rows {
		rows[i] = models.Message{ReceivedAt: old, SourceID: 1, DestinationID: 2, Format: models.MessageFormatData}
	}
	require.NoError(t, database.CreateInBatches(rows, 500).Error)
	cfg.DMR.Retention = config.Retention{Messages: 30}
	res, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 2500, res.Messages)
	assert.Equal(t, int64(0), count(t, database, &models.Message{}))
}
