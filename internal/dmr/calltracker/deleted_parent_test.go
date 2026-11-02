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

package calltracker_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func forEveryBackend(t *testing.T, f func(t *testing.T, database *gorm.DB, ps pubsub.PubSub, store kv.KV)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			memdb.Use(t, &cfg)
			be.Setup(t, &cfg)
			database, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
			require.NoError(t, err)
			store, err := kv.MakeKV(context.TODO(), &cfg)
			require.NoError(t, err)
			t.Cleanup(func() {
				sqlDB, _ := database.DB()
				_ = sqlDB.Close()
				_ = ps.Close()
				_ = store.Close()
			})
			require.NoError(t, database.Create(&models.User{ID: userA, Callsign: "KI5VMF", Username: "a", Approved: true}).Error)
			require.NoError(t, database.Create(&models.User{ID: userB, Callsign: "KD5SMP", Username: "b", Approved: true}).Error)
			require.NoError(t, database.Create(&models.Repeater{
				RepeaterConfiguration: models.RepeaterConfiguration{ID: repeaterA, Callsign: "RPT1"},
				OwnerID:               userA,
				Type:                  models.RepeaterTypeMMDVM,
			}).Error)
			require.NoError(t, database.Create(&models.Talkgroup{ID: tgID, Name: "TG42"}).Error)
			f(t, database, ps, store)
		})
	}
}

// callAcrossADelete starts a voice call of userB to the talkgroup on
// repeaterA and a message of userB to userA on another repeater, runs del,
// and then ends the voice call two seconds after it started and stops the
// tracker, which ends the message.
func callAcrossADelete(t *testing.T, database *gorm.DB, ps pubsub.PubSub, store kv.KV, del func()) {
	t.Helper()
	base := time.Now().Truncate(time.Second)
	fake := clock.NewFake(base)
	ct := calltracker.NewCallTracker(database, ps, store, calltracker.WithClock(fake))
	t.Cleanup(ct.Stop)
	voice := newSender(t, ct, userB, tgID, true, 7001)
	voice.at = base
	voice.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
	for i := range 12 {
		voice.at = base.Add(time.Duration(i+1) * 60 * time.Millisecond)
		voice.send(dmrconst.FrameVoice, uint(i%5)+1, layer2.RawBurst{})
	}
	sms := newSender(t, ct, userB, userA, false, 7002)
	sms.repeater = workerApart(repeaterA)
	sms.at = base
	sms.data(confirmedText(t, "across a delete")...)
	ct.Flush()
	del()
	fake.Advance(2 * time.Second)
	voice.at = fake.Now()
	voice.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
	ct.Flush()
	ct.Stop()
}

// TestACallOfAUserDeletedDuringItEnds deletes the user who is sending a
// voice call and a message, while both are on the air. The delete keeps
// the calls without the user. The ends of the calls must still be saved,
// without the user.
func TestACallOfAUserDeletedDuringItEnds(t *testing.T) {
	t.Parallel()
	forEveryBackend(t, func(t *testing.T, database *gorm.DB, ps pubsub.PubSub, store kv.KV) {
		callAcrossADelete(t, database, ps, store, func() { require.NoError(t, models.DeleteUser(database, userB)) })
		var calls []models.Call
		require.NoError(t, database.Order("id").Find(&calls).Error)
		require.Len(t, calls, 2)
		for _, c := range calls {
			assert.False(t, c.Active, "call %d of stream %d did not end", c.ID, c.StreamID)
			assert.Nil(t, c.UserID, "call %d of stream %d has the deleted user again", c.ID, c.StreamID)
		}
		got := byStream(calls)
		assert.Equal(t, 2*time.Second, got[7001].Duration)
		require.NotNil(t, got[7002].ToUserID)
		assert.Equal(t, userA, *got[7002].ToUserID)
	})
}

// TestASaveOfACallThatADeleteRemovedEnds deletes the repeater of a voice
// call while it is on the air, which deletes the row of the call. The save
// of the end of the call must stop when it finds the row gone, and must not
// write it again.
func TestASaveOfACallThatADeleteRemovedEnds(t *testing.T) {
	t.Parallel()
	forEveryBackend(t, func(t *testing.T, database *gorm.DB, ps pubsub.PubSub, store kv.KV) {
		var reads atomic.Int64
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:count_call_locks", func(d *gorm.DB) {
			if _, ok := d.Statement.Clauses["FOR"]; ok && d.Statement.Table == "calls" {
				reads.Add(1)
			}
		}))
		base := time.Now().Truncate(time.Second)
		fake := clock.NewFake(base)
		ct := calltracker.NewCallTracker(database, ps, store, calltracker.WithClock(fake))
		t.Cleanup(ct.Stop)
		voice := newSender(t, ct, userB, tgID, true, 7001)
		voice.at = base
		voice.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
		for i := range 12 {
			voice.at = base.Add(time.Duration(i+1) * 60 * time.Millisecond)
			voice.send(dmrconst.FrameVoice, uint(i%5)+1, layer2.RawBurst{})
		}
		ct.Flush()
		require.NoError(t, models.DeleteRepeater(database, repeaterA))
		fake.Advance(2 * time.Second)
		voice.at = fake.Now()
		voice.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		ct.Flush()
		after := reads.Load()
		for range 120 {
			fake.Advance(time.Second)
			ct.Flush()
		}
		assert.Equal(t, after, reads.Load(), "the save of a deleted call goes on")
		var calls []models.Call
		require.NoError(t, database.Unscoped().Find(&calls).Error)
		assert.Empty(t, calls, "a deleted call was written again")
	})
}
