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
)

// TestAddCodecColumnsKeepsEverySlotAMBE migrates a database from before
// transcoding. Its repeaters must come out with every timeslot on
// Automatic and AMBE, its peers AMBE only, and its calls without a codec.
func TestAddCodecColumnsKeepsEverySlotAMBE(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
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

			m := database.Migrator()
			for model, cols := range map[any][]string{
				&models.Repeater{}: {"TS1Codec", "TS2Codec", "TS1HeardCodec", "TS2HeardCodec", "TS1HeardCodec2Mode", "TS2HeardCodec2Mode", "TS1HeardAt", "TS2HeardAt"},
				&models.Peer{}:     {"Codec2"},
				&models.Call{}:     {"Codec", "Codec2Mode", "SentAMBE", "SentCodec2", "MissedMS", "TranscodeBusy"},
			} {
				for _, col := range cols {
					require.NoError(t, m.DropColumn(model, col), col)
				}
			}
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610241000").Error)

			require.NoError(t, database.Create(&models.User{ID: 1000001, Callsign: "N0CALL", Username: "old"}).Error)
			require.NoError(t, database.Exec(
				"INSERT INTO repeaters (id, callsign, type, password, owner_id) VALUES (?, ?, ?, ?, ?)", 311860, "N0CALL", models.RepeaterTypeMMDVM, "s3cret", 1000001,
			).Error)
			require.NoError(t, database.Exec(
				"INSERT INTO peers (id, ip, port, password, owner_id, ingress, egress) VALUES (?, ?, ?, ?, ?, ?, ?)", 311999, "192.0.2.20", 62044, "s3cret", 1000001, true, true,
			).Error)
			require.NoError(t, database.Exec(
				"INSERT INTO calls (stream_id, start_time, duration, active, time_slot, group_call, destination_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
				7, time.Now(), int64(time.Second), false, false, true, 91, time.Now(), time.Now(),
			).Error)

			require.NoError(t, migration.Migrate(database, &cfg))

			var rpt models.Repeater
			require.NoError(t, database.First(&rpt, 311860).Error)
			assert.Equal(t, "N0CALL", rpt.Callsign)
			assert.Equal(t, models.CodecSettingAuto, rpt.TS1Codec)
			assert.Equal(t, models.CodecSettingAuto, rpt.TS2Codec)
			assert.Empty(t, rpt.TS1HeardCodec)
			assert.Nil(t, rpt.TS1HeardAt)
			assert.Equal(t, models.CodecAMBE, rpt.SlotCodec(false))
			assert.Equal(t, models.CodecAMBE, rpt.SlotCodec(true))

			var peer models.Peer
			require.NoError(t, database.First(&peer, 311999).Error)
			assert.False(t, peer.Codec2)

			var call models.Call
			require.NoError(t, database.First(&call).Error)
			assert.Empty(t, call.Codec)
			assert.Zero(t, call.SentAMBE+call.SentCodec2+call.MissedMS+call.Codec2Mode)
			assert.False(t, call.TranscodeBusy)
		})
	}
}

func TestSlotCodec(t *testing.T) {
	t.Parallel()
	r := models.Repeater{Type: models.RepeaterTypeMMDVM, TS1Codec: models.CodecSettingAuto, TS2Codec: models.CodecSettingAuto}
	assert.Equal(t, models.CodecAMBE, r.SlotCodec(false), "a slot that heard nothing is AMBE")
	r.TS1HeardCodec = models.CodecCodec2
	assert.Equal(t, models.CodecCodec2, r.SlotCodec(false))
	assert.Equal(t, models.CodecAMBE, r.SlotCodec(true), "the other slot keeps its codec")
	r.TS1Codec = models.CodecAMBE
	assert.Equal(t, models.CodecAMBE, r.SlotCodec(false), "the setting overrides what was heard")
	r.TS2Codec = models.CodecCodec2
	assert.Equal(t, models.CodecCodec2, r.SlotCodec(true))
	r.Type = models.RepeaterTypeIPSC
	assert.Equal(t, models.CodecAMBE, r.SlotCodec(true), "IPSC is AMBE only")
}

// TestRepeaterCodecQueries sets the codec of repeater timeslots and checks
// the repeaters that have a Codec2 timeslot and those of a talkgroup. A
// save of a stale copy of a repeater, as the repeater servers do at a
// login, must not undo the codec.
func TestRepeaterCodecQueries(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, database.Create(&models.User{ID: 1000001, Callsign: "N0CALL", Username: "old"}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: 91, Name: "TG"}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: 92, Name: "TG"}).Error)
	tg91 := uint(91)
	for _, r := range []models.Repeater{
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100001}, OwnerID: 1000001, Type: models.RepeaterTypeMMDVM},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002}, OwnerID: 1000001, Type: models.RepeaterTypeMMDVM, TS2DynamicTalkgroupID: &tg91},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100003}, OwnerID: 1000001, Type: models.RepeaterTypeIPSC},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100004}, OwnerID: 1000001, Type: models.RepeaterTypeMMDVM},
	} {
		require.NoError(t, database.Create(&r).Error)
	}
	require.NoError(t, models.SetRepeaterStaticTalkgroups(database, 100001, []models.Talkgroup{{ID: 91}}, nil))
	require.NoError(t, models.SetRepeaterStaticTalkgroups(database, 100003, nil, []models.Talkgroup{{ID: 91}}))
	require.NoError(t, models.SetRepeaterStaticTalkgroups(database, 100004, []models.Talkgroup{{ID: 92}}, nil))

	ids, err := models.TalkgroupRepeaterIDs(database, 91)
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint{100001, 100002, 100003}, ids)

	ids, err = models.Codec2RepeaterIDs(database)
	require.NoError(t, err)
	assert.Empty(t, ids, "every timeslot starts AMBE")

	stale, err := models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	at := time.Date(2026, 10, 24, 19, 2, 0, 0, time.UTC)
	require.NoError(t, models.SetRepeaterHeardCodec(database, 100001, true, models.CodecCodec2, 3200, at))
	require.NoError(t, models.SetRepeaterCodec(database, 100004, models.CodecCodec2, models.CodecSettingAuto))
	require.NoError(t, models.SetRepeaterHeardCodec(database, 100003, false, models.CodecCodec2, 0, at))
	ids, err = models.Codec2RepeaterIDs(database)
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint{100001, 100004}, ids, "a heard Codec2 slot and a slot set to Codec2; IPSC is AMBE only")

	stale.SimplexRepeater = true
	require.NoError(t, database.Save(&stale).Error)
	fresh, err := models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	assert.True(t, fresh.SimplexRepeater)
	assert.Equal(t, models.CodecCodec2, fresh.TS2HeardCodec, "a save of a stale copy must not undo the codec heard")
	assert.Equal(t, 3200, fresh.TS2HeardCodec2Mode)
	require.NotNil(t, fresh.TS2HeardAt)
	assert.True(t, at.Equal(*fresh.TS2HeardAt))
	assert.Equal(t, models.CodecCodec2, fresh.SlotCodec(true))

	require.NoError(t, models.SetRepeaterHeardCodec(database, 100001, true, models.CodecAMBE, 0, at))
	fresh, err = models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	assert.Equal(t, models.CodecAMBE, fresh.SlotCodec(true), "an AMBE call clears the slot")
	assert.Equal(t, 3200, fresh.TS2HeardCodec2Mode, "the Codec2 mode stays for the next Codec2 call")

	set, err := models.FindRepeaterByID(database, 100004)
	require.NoError(t, err)
	require.NoError(t, models.SetRepeaterCodec(database, 100004, models.CodecSettingAuto, models.CodecAMBE))
	set.City = "Austin"
	require.NoError(t, database.Save(&set).Error)
	fresh, err = models.FindRepeaterByID(database, 100004)
	require.NoError(t, err)
	assert.Equal(t, "Austin", fresh.City)
	assert.Equal(t, models.CodecSettingAuto, fresh.TS1Codec, "a save of a stale copy must not undo the codec setting")
	assert.Equal(t, models.CodecAMBE, fresh.TS2Codec)
}
