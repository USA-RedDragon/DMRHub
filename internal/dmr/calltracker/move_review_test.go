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
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
