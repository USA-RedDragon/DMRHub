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
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func holdSharedSave(t *testing.T, database *gorm.DB) (entered <-chan struct{}, release func()) {
	t.Helper()
	in, hold := make(chan struct{}), make(chan struct{})
	var armed atomic.Bool
	armed.Store(true)
	require.NoError(t, database.Callback().Query().Before("gorm:query").Register("test:hold_save", func(d *gorm.DB) {
		if _, ok := d.Statement.Clauses["FOR"]; ok && armed.CompareAndSwap(true, false) {
			close(in)
			<-hold
		}
	}))
	release = sync.OnceFunc(func() { close(hold) })
	t.Cleanup(release)
	return in, release
}

// TestASlowSharedSaveLeavesTheWorkerFree holds the save of a shared voice
// call in its transaction, as a starved database does. The worker of the
// slot must go on with the next task while the save waits.
func TestASlowSharedSaveLeavesTheWorkerFree(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		entered, release := holdSharedSave(t, database)
		defer release()
		s := newSender(t, ct, userA, tgID, true, 9401)
		s.voiceCall(1, false)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		<-entered

		answered := calltracker.WorkerAnswers(ct, s.server, s.repeater, s.slot)
		synctest.Wait()
		select {
		case <-answered:
		default:
			t.Fatal("the worker waits for the save of a shared call")
		}
		release()
		c := waitDone(t, ct, database, 1)[0]
		assert.Equal(t, uint(8), c.TotalPackets)
	})
}

// TestAnEndDuringASharedSaveKeepsBothParts holds the save of a call that
// hung, and then the call goes on and ends while the save waits. The row
// must hold the bursts of both parts and end inactive.
func TestAnEndDuringASharedSaveKeepsBothParts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		entered, release := holdSharedSave(t, database)
		defer release()
		s := newSender(t, ct, userA, tgID, true, 9402)
		s.voiceCall(1, false)
		time.Sleep(calltracker.VoiceHang + time.Second)
		<-entered

		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		synctest.Wait()
		release()
		c := waitDone(t, ct, database, 1)[0]
		assert.Equal(t, uint(14), c.TotalPackets)
		assert.False(t, c.Active)
	})
}

// TestASaveThatGivesUpWaitsLongerEachTime makes the first two saves of a
// shared call outlast saveTimeout, as a database too slow to save in time
// does. Each save that gives up must double the pause before the next
// one, so that saves that cannot end in time stop taking the writes of
// the database, and the third save stores the call.
func TestASaveThatGivesUpWaitsLongerEachTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		sqlDB, err := database.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(2)
		keep, err := sqlDB.Conn(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { _ = keep.Close() })
		var mu sync.Mutex
		var starts []time.Time
		require.NoError(t, database.Callback().Query().Before("gorm:query").Register("test:slow_save", func(d *gorm.DB) {
			if _, ok := d.Statement.Clauses["FOR"]; !ok {
				return
			}
			mu.Lock()
			starts = append(starts, time.Now())
			n := len(starts)
			mu.Unlock()
			if n <= 2 {
				time.Sleep(calltracker.SaveTimeout + time.Second)
			}
		}))
		s := newSender(t, ct, userA, tgID, true, 9403)
		s.voiceCall(1, false)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		time.Sleep(time.Minute)
		c := waitDone(t, ct, database, 1)[0]
		assert.Equal(t, uint(8), c.TotalPackets)

		mu.Lock()
		defer mu.Unlock()
		require.Len(t, starts, 3, "saves of the call")
		stall := calltracker.SaveTimeout + time.Second
		assert.GreaterOrEqual(t, starts[1].Sub(starts[0]), stall+calltracker.SaveTimeout, "pause after the first save gave up")
		assert.GreaterOrEqual(t, starts[2].Sub(starts[1]), stall+2*calltracker.SaveTimeout, "pause after the second save gave up")
	})
}
