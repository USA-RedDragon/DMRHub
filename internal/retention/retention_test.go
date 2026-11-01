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
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
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
	memdb.Use(&cfg)
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
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
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
	_, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
	require.NoError(t, err)
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

func TestRetentionPrunesPositionsAtTheirRadiosRetentionAndTheHubsMost(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	now := time.Now()
	cfg.DMR.Positions.MaxRetention = 3
	for _, radio := range []uint{3191868, 3140598} {
		require.NoError(t, database.Create(&models.User{ID: radio, Callsign: fmt.Sprintf("N%dX", radio%100), Username: fmt.Sprint(radio), Approved: true}).Error)
		require.NoError(t, models.SetPositionTracking(database, radio, radio, true, now.Add(-30*daySpan)))
		for _, age := range []time.Duration{time.Hour, 2 * daySpan, 5 * daySpan} {
			require.NoError(t, database.Create(&models.Position{RadioID: radio, Latitude: 30, Longitude: -97, Format: models.PositionFormatLRRP, ReceivedAt: now.Add(-age)}).Error)
		}
	}
	require.NoError(t, models.SetPositionRetention(database, 3140598, 7*daySpan))
	_, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count(t, database, &models.Position{}, "radio_id = 3191868"), "24 hours")
	assert.Equal(t, int64(2), count(t, database, &models.Position{}, "radio_id = 3140598"), "7 days, cut to the hub's 3")
}

func TestRetentionPurgesTheMessageAccessLogWithTheMessages(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			for _, days := range []int{0, 30} {
				t.Run(fmt.Sprintf("%d days", days), func(t *testing.T) {
					cfg, database, kvStore := setup(t, be)
					now := time.Now()
					msgs := []models.Message{{SourceID: 3100001, DestinationID: 3100002}}
					for _, age := range []int{1, 20, 40, 100} {
						require.NoError(t, models.LogMessageAccess(database, 1, models.MessageAccessSearch, nil, "", msgs, now.Add(-time.Duration(age)*daySpan)))
					}
					cfg.DMR.Retention = config.Retention{Messages: days}

					res, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
					require.NoError(t, err)
					if days == 0 {
						assert.Equal(t, 0, res.MessageAccesses)
						assert.Equal(t, int64(4), count(t, database, &models.MessageAccess{}), "0 keeps the log")
						assert.Equal(t, int64(8), count(t, database, &models.MessageAccessRadioID{}))
						return
					}
					assert.Equal(t, 2, res.MessageAccesses)
					assert.Equal(t, int64(2), count(t, database, &models.MessageAccess{}))
					assert.Equal(t, int64(0), count(t, database, &models.MessageAccess{}, "at < ?", now.Add(-30*daySpan)))
					assert.Equal(t, int64(4), count(t, database, &models.MessageAccessRadioID{}), "the radio IDs of a purged entry go with it")
				})
			}
		})
	}
}

// TestRetentionPurgesThePartsWithTheirTransmissions purges an old
// transmission that two parts were merged into, as a call that moved
// between replicas is, and keeps a new one with its part.
func TestRetentionPurgesThePartsWithTheirTransmissions(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, database, kvStore := setup(t, be)
			now := time.Now()
			old := models.Call{StartTime: now.Add(-50 * daySpan), Kind: models.CallKindVoice}
			kept := models.Call{StartTime: now.Add(-daySpan), Kind: models.CallKindVoice}
			require.NoError(t, database.Create(&old).Error)
			require.NoError(t, database.Create(&kept).Error)
			require.NoError(t, database.Create(&[]models.CallPart{{CallID: old.ID, PartID: "a/1"}, {CallID: old.ID, PartID: "b/1"}, {CallID: kept.ID, PartID: "a/2"}}).Error)
			cfg.DMR.Retention = config.Retention{Transmissions: 30}

			_, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
			require.NoError(t, err)
			assert.Equal(t, int64(0), count(t, database, &models.CallPart{}, "call_id = ?", old.ID))
			assert.Equal(t, int64(1), count(t, database, &models.CallPart{}, "call_id = ?", kept.ID))
		})
	}
}

// TestRetentionPurgesExpiredCosysopInvites keeps the co-sysops and the
// invites that still wait, and purges the expired invites.
func TestRetentionPurgesExpiredCosysopInvites(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, database, kvStore := setup(t, be)
			now := time.Now()
			for _, id := range []uint{1000001, 1000002, 1000003, 1000004} {
				require.NoError(t, database.Create(&models.User{ID: id, Callsign: fmt.Sprintf("N%dC", id), Username: fmt.Sprintf("u%d", id), Password: "hash"}).Error)
			}
			require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100000101}, OwnerID: 1000001, Password: "pw"}).Error)
			old := now.Add(-models.CosysopInviteLifetime - time.Hour)
			_, err := models.InviteCosysop(database, 100000101, 1000002, 1000001, old)
			require.NoError(t, err)
			_, err = models.InviteCosysop(database, 100000101, 1000003, 1000001, old)
			require.NoError(t, err)
			ok, err := models.AnswerCosysopInvite(database, 100000101, 1000003, true, old)
			require.NoError(t, err)
			require.True(t, ok)
			_, err = models.InviteCosysop(database, 100000101, 1000004, 1000001, now)
			require.NoError(t, err)

			res, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
			require.NoError(t, err)
			assert.Equal(t, 1, res.CosysopInvites)
			assert.Equal(t, int64(0), count(t, database, &models.RepeaterCosysop{}, "user_id = ?", 1000002))
			assert.Equal(t, int64(1), count(t, database, &models.RepeaterCosysop{}, "user_id = ?", 1000003))
			assert.Equal(t, int64(1), count(t, database, &models.RepeaterCosysop{}, "user_id = ?", 1000004))
		})
	}
}

// TestRetentionPurgesTheConnectionLogAfterADay keeps the entries of the
// connection logs whose last event is within models.RepeaterEventWindow.
func TestRetentionPurgesTheConnectionLogAfterADay(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, database, kvStore := setup(t, be)
			now := time.Now()
			require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100000101}, Password: "pw"}).Error)
			for _, ev := range []models.RepeaterEvent{
				{RepeaterID: 100000101, Kind: "timeout", LastAt: now.Add(-models.RepeaterEventWindow - time.Minute)},
				{RepeaterID: 100000101, Kind: "connected", LastAt: now.Add(-models.RepeaterEventWindow + time.Minute)},
			} {
				_, err := models.NoteRepeaterEvent(database, ev, 1)
				require.NoError(t, err)
			}

			res, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
			require.NoError(t, err)
			assert.Equal(t, 1, res.RepeaterEvents)
			assert.Equal(t, int64(1), count(t, database, &models.RepeaterEvent{}, "kind = ?", "connected"))
		})
	}
}

// TestRetentionPurgesTheHeldTextsWithTheirMessages purges an old message
// that store and forward held, and keeps a new one with its held text.
func TestRetentionPurgesTheHeldTextsWithTheirMessages(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, database, kvStore := setup(t, be)
			now := time.Now()
			require.NoError(t, database.Create(&models.User{ID: 3191868, Callsign: "KI5VMF", Username: "held"}).Error)
			for _, age := range []int{40, 1} {
				at := now.Add(-time.Duration(age) * daySpan)
				m := models.Message{ReceivedAt: at, SourceID: 3140598, DestinationID: 3191868, Format: models.MessageFormatAnyTone}
				require.NoError(t, database.Create(&m).Error)
				require.NoError(t, database.Create(&models.HeldMessage{MessageID: m.ID, RadioID: 3191868, UserID: 3191868, State: models.HeldStateExpired, HeldAt: at}).Error)
			}
			cfg.DMR.Retention = config.Retention{Messages: 30}

			_, err := retention.NewPurger(cfg, database, kvStore).Run(context.Background(), now)
			require.NoError(t, err)
			assert.Equal(t, int64(1), count(t, database, &models.Message{}))
			assert.Equal(t, int64(1), count(t, database, &models.HeldMessage{}))
			assert.Equal(t, int64(0), count(t, database, &models.HeldMessage{}, "held_at < ?", now.Add(-30*daySpan)))
		})
	}
}
