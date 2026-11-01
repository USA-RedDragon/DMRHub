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
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type armedKV struct {
	kv.KV
	failNext atomic.Int32
	down     atomic.Bool
}

func (f *armedKV) Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	if f.down.Load() || f.failNext.Add(-1) >= 0 {
		return errors.New("the KV store is down")
	}
	f.failNext.Store(0)
	return f.KV.Update(ctx, key, ttl, fn) //nolint:wrapcheck
}

func isMergedSave(db *gorm.DB) bool {
	return db.Statement.Table == "calls" && slices.Contains(db.Statement.Selects, "*")
}

func armedReplicas(t *testing.T) (*calltracker.CallTracker, *calltracker.CallTracker, *gorm.DB, *armedKV) {
	t.Helper()
	database, ps, kvStore := trackerDeps(t)
	shared := &armedKV{KV: kvStore}
	one := calltracker.NewCallTracker(database, ps, shared)
	t.Cleanup(one.Stop)
	two := calltracker.NewCallTracker(database, ps, shared)
	t.Cleanup(two.Stop)
	return one, two, database, shared
}

// TestAFailedEndOfASaveCountsThePartsOnce fails the KV Update that ends
// the merged save of a moved call, right after the save committed, so the
// parts stay in the shared record and are saved again. The database
// stores each part once, so the row counts each burst once.
func TestAFailedEndOfASaveCountsThePartsOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, shared := armedReplicas(t)
		var saves atomic.Int32
		require.NoError(t, database.Callback().Update().After("gorm:update").Register("test:arm", func(db *gorm.DB) {
			if isMergedSave(db) && saves.Add(1) == 1 {
				shared.failNext.Store(1)
			}
		}))
		s := newSender(t, one, userA, tgID, true, 9101)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		for range 20 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 14, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.Equal(t, uint(14), c.TotalPackets, "packets")
	})
}

// TestAStalledSaverCountsThePartsOnce stalls the worker that saved the
// merged row of a moved call for longer than the save lock, right after
// the save, so the other replica saves the parts again. The database
// stores each part once, so the row counts each burst once.
func TestAStalledSaverCountsThePartsOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, _ := armedReplicas(t)
		var saves atomic.Int32
		require.NoError(t, database.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:stall", func(db *gorm.DB) {
			if isMergedSave(db) && saves.Add(1) == 1 {
				time.Sleep(6 * time.Second)
			}
		}))
		s := newSender(t, one, userA, tgID, true, 9102)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		for range 20 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 14, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.Equal(t, uint(14), c.TotalPackets, "packets")
	})
}

// TestAWaitingPartOutlivesALongKVOutage ends a moved call while the KV
// store is down for longer than its records live. The part of the old
// replica, which waited in the shared record, still reaches the row.
func TestAWaitingPartOutlivesALongKVOutage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, shared := armedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9103)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		for range 12 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		one.Flush()
		shared.down.Store(true)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		for range 70 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}
		shared.down.Store(false)
		for range 10 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 7+12*6+1, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.False(t, c.Active)
	})
}

// TestAWaitingPartOutlivesAShortKVOutage ends a moved call while the KV
// store is down for 10 s. Both parts reach the row.
func TestAWaitingPartOutlivesAShortKVOutage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, shared := armedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9104)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		for range 12 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		one.Flush()
		shared.down.Store(true)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		for range 10 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}
		shared.down.Store(false)
		for range 10 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 7+12*6+1, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.False(t, c.Active)
	})
}

// TestAKVBlipAtTheEndOfALongPartKeepsEveryPart ends a long second part of
// a moved call while the KV store is down for 1.5 s. Both parts reach the
// row.
func TestAKVBlipAtTheEndOfALongPartKeepsEveryPart(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, shared := armedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9106)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		for range 200 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		one.Flush()
		shared.down.Store(true)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		time.Sleep(1500 * time.Millisecond)
		shared.down.Store(false)
		for range 15 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 7+200*6+1, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.False(t, c.Active)
	})
}

// TestAStarvedOldReplicaJoinsTheRowLate holds the worker of the old
// replica of a moved call for longer than a part counts as active, so the
// new replica saves the row first. Once the old worker runs again, its
// part joins the row, and the row counts every burst once.
func TestAStarvedOldReplicaJoinsTheRowLate(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, _ := armedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9105)
		s.voiceCall(2, false)
		one.Flush()
		release := calltracker.HoldWorker(one, s.server, s.repeater, s.slot)
		t.Cleanup(release)
		for range 2 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		time.Sleep(8 * time.Second)
		two.Flush()
		release()
		for range 15 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 1+12+12+6+1, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.Equal(t, uint(1+12+12+6+1), c.TotalPackets, "packets")
		assert.False(t, c.Active)
	})
}

type hungKV struct {
	kv.KV
	hung atomic.Bool
}

func (h *hungKV) Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	if h.hung.Load() {
		<-ctx.Done()
		return ctx.Err() //nolint:wrapcheck
	}
	return h.KV.Update(ctx, key, ttl, fn) //nolint:wrapcheck
}

// TestAHungKVDoesNotStallTheTrackingOfCalls runs 24 calls on one worker
// while every KV Update hangs until its timeout, as a KV store that stops
// answering does. The worker waits on the store at most once in a while,
// so it keeps up with the bursts and drops none.
func TestAHungKVDoesNotStallTheTrackingOfCalls(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, kvStore := trackerDeps(t)
		shared := &hungKV{KV: kvStore}
		ct := calltracker.NewCallTracker(database, ps, shared)
		t.Cleanup(ct.Stop)
		worker := calltracker.WorkerOf(models.RepeaterTypeMMDVM, repeaterA, true)
		var senders []*sender
		for r := repeaterA; len(senders) < 24; r++ {
			if calltracker.WorkerOf(models.RepeaterTypeMMDVM, r, true) != worker {
				continue
			}
			s := newSender(t, ct, userA, tgID, true, 9200+uint(len(senders)))
			s.repeater = r
			senders = append(senders, s)
		}
		for _, s := range senders {
			s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
		}
		ct.Flush()
		shared.hung.Store(true)
		for range 100 {
			for _, s := range senders {
				sendSuperframe(s)
			}
			time.Sleep(superframeTime)
		}
		for _, s := range senders {
			s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		}
		ct.Flush()
		shared.hung.Store(false)

		assert.Zero(t, ct.Dropped(), "the worker dropped bursts while it waited on the KV store")
		for _, c := range waitDone(t, ct, database, len(senders)) {
			assert.Equal(t, 1+600+1, len(c.CallData)/layer2.RawBurstSize, "bursts of the call to %d", c.DestinationID)
		}
	})
}
