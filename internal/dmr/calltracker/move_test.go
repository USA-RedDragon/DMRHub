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
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type callEvents struct {
	mu   sync.Mutex
	last map[uint]models.Call
}

func watchCalls(t *testing.T, ps pubsub.PubSub) *callEvents {
	t.Helper()
	sub := ps.Subscribe("calls")
	ev := &callEvents{last: map[uint]models.Call{}}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case msg := <-sub.Channel():
				var c models.Call
				if json.Unmarshal(msg, &c) == nil {
					ev.mu.Lock()
					ev.last[c.ID] = c
					ev.mu.Unlock()
				}
			case <-done:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(done)
		_ = sub.Close()
	})
	return ev
}

func newSharedReplicas(t *testing.T) (*calltracker.CallTracker, *calltracker.CallTracker, *gorm.DB, *callEvents) {
	t.Helper()
	database, ps, kvStore := trackerDeps(t)
	ev := watchCalls(t, ps)
	one := calltracker.NewCallTracker(database, ps, kvStore)
	t.Cleanup(one.Stop)
	two := calltracker.NewCallTracker(database, ps, kvStore)
	t.Cleanup(two.Stop)
	return one, two, database, ev
}

func lastEvent(t *testing.T, ev *callEvents, id uint) models.Call {
	t.Helper()
	synctest.Wait()
	ev.mu.Lock()
	defer ev.mu.Unlock()
	last, ok := ev.last[id]
	require.True(t, ok, "no event of the row")
	return last
}

func requireEventMatches(t *testing.T, sub *callEvents, row models.Call) {
	t.Helper()
	ev := lastEvent(t, sub, row.ID)
	assert.False(t, ev.Active, "the last event of the row is not its end")
	assert.Equal(t, row.Duration, ev.Duration, "the last event of the row has another duration than the database")
	assert.InDelta(t, row.Loss, ev.Loss, 1e-6, "the last event of the row has another loss than the database")
}

func sendSuperframe(s *sender) {
	s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
	for v := uint(1); v <= 5; v++ {
		s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
	}
}

// TestAMovedCallContinuesTheSavedRowOfItsOldReplica moves a call to
// another replica after the old replica ended and saved its part on the
// hang, as a slow move does. The new replica continues the same row.
func TestAMovedCallContinuesTheSavedRowOfItsOldReplica(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, sub := newSharedReplicas(t)

		s := newSender(t, one, userA, tgID, true, 7070)
		s.voiceCall(2, false)
		time.Sleep(calltracker.VoiceHang + time.Second)
		one.Flush()
		ended := rows(t, database)
		require.Len(t, ended, 1)
		require.False(t, ended[0].Active)

		s.ct = two
		sendSuperframe(s)
		two.Flush()
		resumed := rows(t, database)
		require.Len(t, resumed, 1)
		assert.True(t, resumed[0].Active, "the row is active again on the new replica")
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		c := waitDone(t, two, database, 1)[0]
		assert.Equal(t, ended[0].ID, c.ID)
		assert.Zero(t, c.Loss)
		assert.Len(t, c.CallData, 20*layer2.RawBurstSize)
		assert.GreaterOrEqual(t, c.Duration, calltracker.VoiceHang+time.Second)
		requireEventMatches(t, sub, c)
	})
}

// TestAMovedCallEndsOnTheOldReplicaLast ends a moved call on the new
// replica while the worker of the old replica is held, so that the old
// replica ends its part on the hang after the new one ended. The row holds
// both parts once.
func TestAMovedCallEndsOnTheOldReplicaLast(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, sub := newSharedReplicas(t)

		s := newSender(t, one, userA, tgID, true, 7071)
		s.voiceCall(1, false)
		one.Flush()
		release := calltracker.HoldWorker(one, s.server, s.repeater, s.slot)
		t.Cleanup(release)
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		time.Sleep(calltracker.VoiceHang + time.Second)
		release()

		c := waitDone(t, one, database, 1)[0]
		assert.Zero(t, c.Loss)
		assert.Len(t, c.CallData, 14*layer2.RawBurstSize)
		requireEventMatches(t, sub, c)
	})
}

// TestAMovedCallThatEndsWhileTheOldReplicaSavesIsFolded ends a moved call
// on the new replica while the old replica is between its save of the row
// and the end of that save in the KV store. The old replica saves the row
// again with the part of the new one, so the row holds both parts once.
func TestAMovedCallThatEndsWhileTheOldReplicaSavesIsFolded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, sub := newSharedReplicas(t)
		var armed atomic.Bool
		gate := make(chan struct{})
		held := make(chan struct{})
		require.NoError(t, database.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:hold", func(db *gorm.DB) {
			if db.Statement.Table == "calls" && armed.CompareAndSwap(true, false) {
				close(held)
				<-gate
			}
		}))

		s := newSender(t, one, userA, tgID, true, 7072)
		s.voiceCall(2, false)
		one.Flush()
		armed.Store(true)
		<-held

		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		gate <- struct{}{}

		c := waitDone(t, one, database, 1)[0]
		assert.Zero(t, c.Loss)
		assert.Len(t, c.CallData, 20*layer2.RawBurstSize)
		requireEventMatches(t, sub, c)
	})
}
