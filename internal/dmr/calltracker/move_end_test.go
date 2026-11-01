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

	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
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
		ct.Missed(term, 60)
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
