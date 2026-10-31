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
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const superframeTime = 360 * time.Millisecond

func oneRow(t *testing.T, _ *calltracker.CallTracker, rs []models.Call) models.Call {
	t.Helper()
	ids := make([]uint, 0, len(rs))
	for _, c := range rs {
		ids = append(ids, c.ID)
	}
	require.Len(t, ids, 1, "rows of the call")
	return rs[0]
}

// TestALongCallThatMovesIsOneRow moves a call to another replica after it
// ran for longer than a minute on the first one. The shared record of the
// call lives as long as the call, so the call is one row with every burst.
func TestALongCallThatMovesIsOneRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, sub := newSharedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9002)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
		for range 200 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		one.Flush()
		s.ct = two
		for range 3 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		c := oneRow(t, one, waitDone(t, two, database, 1))
		assert.Equal(t, 1+203*6+1, len(c.CallData)/layer2.RawBurstSize, "bursts")
		requireEventMatches(t, sub, c)
	})
}

// TestAMovedCallWithALongSecondPartIsOneRow moves a call to another
// replica soon after it started and lets it run there for longer than a
// minute. The part of the first replica waits in the shared record for
// that time, and the row keeps it and the start of the call.
func TestAMovedCallWithALongSecondPartIsOneRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, sub := newSharedReplicas(t)
		start := time.Now()
		s := newSender(t, one, userA, tgID, true, 9003)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
		for range 5 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		one.Flush()
		s.ct = two
		for range 200 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		c := oneRow(t, one, waitDone(t, two, database, 1))
		assert.Equal(t, 1+205*6+1, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.True(t, c.StartTime.Equal(start), "the row starts at %s, the call at %s", c.StartTime, start)
		requireEventMatches(t, sub, c)
	})
}

// TestADeadReplicaDoesNotHoldAMovedRowOpen moves a call to another replica
// while the worker of the first one stops for good, as a replica that dies
// does. The part of the dead replica stops counting as active once it
// stops sending bursts, so the row ends with the part of the new replica.
func TestADeadReplicaDoesNotHoldAMovedRowOpen(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, _ := newSharedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9001)
		s.voiceCall(2, false)
		one.Flush()
		release := calltracker.HoldWorker(one, s.server, s.repeater, s.slot)
		t.Cleanup(release)
		s.ct = two
		for range 3 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		time.Sleep(30 * time.Second)
		two.Flush()

		c := oneRow(t, two, rows(t, database))
		assert.False(t, c.Active, "a dead replica holds the row open")
		assert.GreaterOrEqual(t, len(c.CallData), (3*6+1)*layer2.RawBurstSize)
	})
}

// TestAMovedCallAfterItsRecordExpiredIsOneRow ends a call on the hang
// after it ran for longer than a minute, and moves the rest of it to
// another replica, which runs it for longer than a minute. The row ends
// with both parts and the last event of it matches the database.
func TestAMovedCallAfterItsRecordExpiredIsOneRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, sub := newSharedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9005)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
		for range 280 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		time.Sleep(calltracker.VoiceHang + time.Second)
		one.Flush()
		s.ct = two
		for range 200 {
			sendSuperframe(s)
			time.Sleep(superframeTime)
		}
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		c := oneRow(t, one, waitDone(t, two, database, 1))
		assert.Equal(t, 1+480*6+1, len(c.CallData)/layer2.RawBurstSize, "bursts")
		requireEventMatches(t, sub, c)
	})
}

// TestANoteAfterAMergedSaveKeepsTheMergedRow marks the codec of a moved
// call after the merged save of its row. The event that the mark
// publishes shows the merged row, as the database does.
func TestANoteAfterAMergedSaveKeepsTheMergedRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, sub := newSharedReplicas(t)
		s := newSender(t, one, userA, tgID, true, 9004)
		s.voiceCall(1, false)
		s.seq += 4
		sendSuperframe(s)
		time.Sleep(calltracker.VoiceHang + time.Second)
		one.Flush()
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		last := s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		c := waitDone(t, two, database, 1)[0]
		two.Missed(last, 60)
		two.Flush()

		var stored models.Call
		require.NoError(t, database.First(&stored, c.ID).Error)
		assert.Equal(t, 60, stored.MissedMS)
		ev := lastEvent(t, sub, c.ID)
		assert.Equal(t, stored.MissedMS, ev.MissedMS)
		assert.Equal(t, stored.Duration, ev.Duration)
		assert.InDelta(t, stored.Loss, ev.Loss, 1e-6)
		assert.InDelta(t, stored.Jitter, ev.Jitter, 1e-3)
	})
}

// TestAFailedSaveOfAMovedCallKeepsItsParts fails the first save of the
// merged row of a moved call, as a database that is down for a moment
// does. The parts of both replicas stay in the shared record, and a later
// try saves the row with every burst.
func TestAFailedSaveOfAMovedCallKeepsItsParts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, sub := newSharedReplicas(t)
		var fail atomic.Bool
		require.NoError(t, database.Callback().Update().Before("gorm:update").Register("test:fail", func(db *gorm.DB) {
			if db.Statement.Table == "calls" && fail.CompareAndSwap(true, false) {
				_ = db.AddError(errors.New("the database is down"))
			}
		}))

		s := newSender(t, one, userA, tgID, true, 9006)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		fail.Store(true)

		c := oneRow(t, one, waitDone(t, one, database, 1))
		assert.False(t, fail.Load(), "no save failed")
		assert.Equal(t, 14, len(c.CallData)/layer2.RawBurstSize, "bursts")
		requireEventMatches(t, sub, c)
	})
}
