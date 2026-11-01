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
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func failCallReads(t *testing.T, database *gorm.DB) *atomic.Bool {
	t.Helper()
	var down atomic.Bool
	require.NoError(t, database.Callback().Query().Before("gorm:query").Register("test:dbdown", func(d *gorm.DB) {
		if down.Load() && d.Statement.Table == "calls" {
			_ = d.AddError(errors.New("the database is down"))
		}
	}))
	return &down
}

// TestAnEndThatFailsOnTheKVAndTheDatabaseIsSavedLater ends a call while
// the KV store and the database both fail. The KV store comes back first,
// the database 10 s later. The part, and a note that came for it in the
// outage, wait for the database, and the row ends with every burst and the
// note.
func TestAnEndThatFailsOnTheKVAndTheDatabaseIsSavedLater(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, kvStore := trackerDeps(t)
		shared := &armedKV{KV: kvStore}
		ct := calltracker.NewCallTracker(database, ps, shared)
		t.Cleanup(ct.Stop)
		dbDown := failCallReads(t, database)

		s := newSender(t, ct, userA, tgID, true, 9320)
		s.voiceCall(2, false)
		ct.Flush()
		shared.down.Store(true)
		dbDown.Store(true)
		term := s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		ct.Flush()
		ct.Missed(t.Context(), term, 60)
		ct.Flush()
		for range 3 {
			time.Sleep(time.Second)
			ct.Flush()
		}
		shared.down.Store(false)
		for range 7 {
			time.Sleep(time.Second)
			ct.Flush()
		}
		dbDown.Store(false)
		for range 20 {
			time.Sleep(time.Second)
			ct.Flush()
		}

		c := oneRow(t, ct, rows(t, database))
		assert.Equal(t, 14, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.Equal(t, 60, c.MissedMS, "the note of the part")
		assert.False(t, c.Active, "the row stays active")
	})
}

// TestAMovedEndThatFailsOnTheKVAndTheDatabaseIsSavedLater is the same for
// the end of a moved call on its new replica.
func TestAMovedEndThatFailsOnTheKVAndTheDatabaseIsSavedLater(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, shared := armedReplicas(t)
		dbDown := failCallReads(t, database)

		s := newSender(t, one, userA, tgID, true, 9321)
		s.voiceCall(2, false)
		time.Sleep(calltracker.VoiceHang + time.Second)
		one.Flush()
		s.ct = two
		for range 3 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		two.Flush()
		shared.down.Store(true)
		dbDown.Store(true)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		for range 3 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}
		shared.down.Store(false)
		for range 7 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}
		dbDown.Store(false)
		for range 20 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 1+12+18+1, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.False(t, c.Active, "the row stays active")
	})
}

// TestAnUnrelatedKVFailureDoesNotSplitAMovedCall fails one KV Update of
// another call on the new replica of a call 1 s before the call moves
// there. The failure says nothing of the store for the moved call, which
// stays one row.
func TestAnUnrelatedKVFailureDoesNotSplitAMovedCall(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, shared := armedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9330)
		s.voiceCall(1, false)
		one.Flush()
		other := newSender(t, two, userB, tgID, true, 9331)
		other.repeater = repeaterA + 7
		shared.failNext.Store(1)
		other.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
		two.Flush()
		time.Sleep(time.Second)
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		other.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		var moved []uint
		for _, c := range waitDone(t, two, database, 2) {
			if c.StreamID == 9330 {
				moved = append(moved, c.ID)
			}
		}
		assert.Len(t, moved, 1, "rows of the moved call")
	})
}

// TestALateNoteOfASavedPartKeepsTheMergedRow marks a burst of the part of
// the old replica of a moved call undeliverable to Y after the new replica
// merged its part, which a burst undeliverable to X is in, into the row.
// The note adds Y to the row and keeps X.
func TestALateNoteOfASavedPartKeepsTheMergedRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database := newReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9350)
		s.voiceCall(2, false)
		pa := s.packet(dmrconst.FrameVoice, 1, layer2.RawBurst{})
		pa.TrackID = one.Track(context.Background(), pa, s.server, s.peer)
		time.Sleep(calltracker.VoiceHang + time.Second)
		one.Flush()
		s.ct = two
		sendSuperframe(s)
		pb := s.packet(dmrconst.FrameVoice, 2, layer2.RawBurst{})
		pb.TrackID = two.Track(context.Background(), pb, s.server, s.peer)
		two.Flush()
		two.Undeliverable(pb, "X")
		two.Flush()
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		waitDone(t, two, database, 1)

		one.Undeliverable(pa, "Y")
		one.Flush()
		for range 3 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}
		c := oneRow(t, one, rows(t, database))
		assert.ElementsMatch(t, []string{"X", "Y"}, c.Undeliverable)
	})
}

type stallKV struct {
	kv.KV
	arm     atomic.Bool
	held    chan struct{}
	release chan struct{}
}

func newStallKV(store kv.KV) *stallKV {
	return &stallKV{KV: store, held: make(chan struct{}), release: make(chan struct{})}
}

func (f *stallKV) Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	if f.arm.CompareAndSwap(true, false) {
		close(f.held)
		<-f.release
		return f.KV.Update(context.WithoutCancel(ctx), key, ttl, fn) //nolint:wrapcheck
	}
	return f.KV.Update(ctx, key, ttl, fn) //nolint:wrapcheck
}

// TestASaveThatRunsTwicePublishesTheEndOnce stalls the worker that saved
// the merged row of a moved call for 6 s before the KV Update that ends
// the save, so the other replica saves the parts again. That save stores
// nothing new, and the row has one end event.
func TestASaveThatRunsTwicePublishesTheEndOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, kvStore := trackerDeps(t)
		ev := watchCalls(t, ps)
		shared := newStallKV(kvStore)
		go func() {
			<-shared.held
			time.Sleep(6 * time.Second)
			close(shared.release)
		}()
		one := calltracker.NewCallTracker(database, ps, shared)
		t.Cleanup(one.Stop)
		two := calltracker.NewCallTracker(database, ps, shared)
		t.Cleanup(two.Stop)
		var saves atomic.Int32
		require.NoError(t, database.Callback().Update().After("gorm:update").Register("test:arm", func(d *gorm.DB) {
			if isMergedSave(d) && saves.Add(1) == 1 {
				shared.arm.Store(true)
			}
		}))

		s := newSender(t, one, userA, tgID, true, 9360)
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
		assert.Equal(t, uint(14), c.TotalPackets, "packets")
		synctest.Wait()
		ev.mu.Lock()
		defer ev.mu.Unlock()
		assert.Equal(t, 1, ev.ends[c.ID], "end events of the row")
	})
}

// TestASaveThatStallsGivesUpTheRow stalls the save of a shared row while
// it holds the lock of the row, as a replica that loses the database in
// the middle of a save does. The save gives up the row after its timeout,
// so it cannot hold the lock for long, and a later save stores the part.
// A pinned connection keeps the memory database while the timeout drops
// the connection of the save.
func TestASaveThatStallsGivesUpTheRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		memdb.Use(&cfg)
		database, err := db.MakeDB(&cfg)
		require.NoError(t, err)
		sqlDB, err := database.DB()
		require.NoError(t, err)
		keep, err := sqlDB.Conn(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = keep.Close()
			_ = sqlDB.Close()
		})
		ps, err := pubsub.MakePubSub(t.Context(), &cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = ps.Close() })
		ct := calltracker.NewCallTracker(database, ps, memKV(t))
		t.Cleanup(ct.Stop)
		call := models.Call{StartTime: time.Now(), Kind: models.CallKindVoice, Active: true}
		require.NoError(t, database.Create(&call).Error)
		var stall atomic.Bool
		require.NoError(t, database.Callback().Query().Before("gorm:query").Register("test:stall", func(d *gorm.DB) {
			if _, ok := d.Statement.Clauses["FOR"]; ok && stall.CompareAndSwap(true, false) {
				time.Sleep(calltracker.SaveTimeout + time.Second)
			}
		}))

		stall.Store(true)
		assert.False(t, calltracker.SaveMergedPart(ct, &call, "a", 3), "a stalled save kept the row")
		assert.True(t, calltracker.SaveMergedPart(ct, &call, "a", 3))
		var stored models.Call
		require.NoError(t, database.First(&stored, call.ID).Error)
		assert.Equal(t, uint(3), stored.TotalPackets)
	})
}

// TestKVFailuresMinutesApartDoNotStopSharing fails one KV Update of three
// other calls on the new replica of a call, a minute apart, and moves the
// call there right after the last one. Failures that far apart say nothing of the store now, so
// the moved call stays one row.
func TestKVFailuresMinutesApartDoNotStopSharing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, shared := armedReplicas(t)
		for i := range uint(3) {
			other := newSender(t, two, userB, tgID, true, 9370+i)
			other.repeater = repeaterA + 7 + i
			shared.failNext.Store(1)
			other.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
			two.Flush()
			if i < 2 {
				time.Sleep(time.Minute)
			}
		}
		s := newSender(t, one, userA, tgID, true, 9380)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()

		var moved []uint
		for _, c := range waitDone(t, two, database, 4) {
			if c.StreamID == 9380 {
				moved = append(moved, c.ID)
			}
		}
		assert.Len(t, moved, 1, "rows of the moved call")
	})
}

type lostAckPool struct {
	*sql.DB
	lose atomic.Bool
}

func (p *lostAckPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &lostAckTx{Tx: tx, pool: p}, nil
}

func (p *lostAckPool) GetDBConn() (*sql.DB, error) { return p.DB, nil }

type lostAckTx struct {
	*sql.Tx
	pool *lostAckPool
}

func (t *lostAckTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err //nolint:wrapcheck
	}
	if t.pool.lose.CompareAndSwap(true, false) {
		return errors.New("the connection broke before the commit was acknowledged")
	}
	return nil
}

// TestASaveWhoseCommitAckIsLostPublishesTheEnd ends a shared call whose
// save commits while its acknowledgement is lost, as a connection that
// breaks right after the commit does. The worker saves again, finds every
// part stored, and still publishes the end of the row once.
func TestASaveWhoseCommitAckIsLostPublishesTheEnd(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, kvStore := trackerDeps(t)
		ev := watchCalls(t, ps)
		sqlDB, err := database.DB()
		require.NoError(t, err)
		pool := &lostAckPool{DB: sqlDB}
		lossy := database.Session(&gorm.Session{})
		lossy.Statement.ConnPool = pool
		ct := calltracker.NewCallTracker(lossy, ps, kvStore)
		t.Cleanup(ct.Stop)

		s := newSender(t, ct, userA, tgID, true, 9390)
		s.voiceCall(1, false)
		ct.Flush()
		pool.lose.Store(true)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		c := waitDone(t, ct, database, 1)[0]
		for range 3 {
			time.Sleep(time.Second)
			ct.Flush()
		}
		assert.False(t, pool.lose.Load(), "no commit lost its acknowledgement")
		assert.Equal(t, uint(8), c.TotalPackets, "packets")
		synctest.Wait()
		ev.mu.Lock()
		defer ev.mu.Unlock()
		assert.Equal(t, 1, ev.ends[c.ID], "end events of the row")
	})
}
