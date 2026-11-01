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
	"encoding/json"
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

// TestACallThatStopsPastItsHangStaysOneRow stops a call without a
// terminator for longer than the voice hang, as a handover of its
// repeater to a restarted server can, and then sends the rest of its
// stream. The hang ends the row, and the rest of the stream, with the same
// stream ID, source, destination and slot, is the same call: it goes on
// the same row, which is active again in the database and in what the
// tracker publishes, and ends once with the whole call.
func TestACallThatStopsPastItsHangStaysOneRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, ps := newTracker(t)
		sub := ps.Subscribe("calls")
		t.Cleanup(func() { _ = sub.Close() })
		var events []models.Call
		collect := func() {
			synctest.Wait()
			for {
				select {
				case msg := <-sub.Channel():
					var c models.Call
					require.NoError(t, json.Unmarshal(msg, &c))
					events = append(events, c)
				default:
					return
				}
			}
		}

		s := newSender(t, ct, userA, tgID, true, 7060)
		s.voiceCall(1, false)
		s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
		s.send(dmrconst.FrameVoice, 1, layer2.RawBurst{})
		time.Sleep(calltracker.VoiceHang + time.Second)
		ct.Flush()
		ended := rows(t, database)
		require.Len(t, ended, 1)
		assert.False(t, ended[0].Active, "the hang ends the row while no burst comes")
		collect()
		require.NotEmpty(t, events)
		assert.False(t, events[len(events)-1].Active, "the end of the row is published")

		for v := uint(2); v <= 5; v++ {
			s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
		}
		ct.Flush()
		resumed := rows(t, database)
		require.Len(t, resumed, 1)
		assert.True(t, resumed[0].Active, "the row is active again")
		collect()
		assert.True(t, events[len(events)-1].Active, "the row is published active again")

		s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
		for v := uint(1); v <= 5; v++ {
			s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
		}
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		calls := waitDone(t, ct, database, 1)
		assert.Equal(t, ended[0].ID, calls[0].ID)
		assert.Zero(t, calls[0].Loss)
		assert.GreaterOrEqual(t, calls[0].Duration, calltracker.VoiceHang+time.Second)
		collect()
		for _, e := range events {
			assert.Equal(t, ended[0].ID, e.ID, "an event of another row")
		}
		last := events[len(events)-1]
		assert.False(t, last.Active)
		assert.Equal(t, calls[0].Duration, last.Duration)
	})
}

// TestABurstThatCameWithinTheHangKeepsItsCall tracks the next burst of a
// call after the voice hang, stamped with a time within the hang, as a
// burst that the hub held while it checked the claim of the stream, or
// that waited in the socket of a starved server, is. The burst came within
// the hang, so the call has one row.
func TestABurstThatCameWithinTheHangKeepsItsCall(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)

		s := newSender(t, ct, userA, tgID, true, 7061)
		s.voiceCall(1, false)
		ct.Flush()
		s.at = time.Now().Add(calltracker.VoiceHang / 2)
		time.Sleep(calltracker.VoiceHang + time.Second)
		ct.Flush()
		s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
		s.at = time.Time{}
		for v := uint(1); v <= 5; v++ {
			s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
		}
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		calls := waitDone(t, ct, database, 1)
		assert.Zero(t, calls[0].Loss)
	})
}

// TestAQueuedBurstWithinTheHangKeepsTheCallOpen holds the worker of a call
// past the voice hang of its first burst while the next burst of the call,
// which came within the hang, waits in its queue. The worker measures the
// hang on the time that its tasks came, so the call never ends: no event
// of the row is an end before its terminator.
func TestAQueuedBurstWithinTheHangKeepsTheCallOpen(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, ps := newTracker(t)
		sub := ps.Subscribe("calls")
		t.Cleanup(func() { _ = sub.Close() })

		s := newSender(t, ct, userA, tgID, true, 7064)
		s.voiceCall(1, false)
		ct.Flush()
		release := calltracker.HoldWorker(ct, s.server, s.repeater, s.slot)
		t.Cleanup(release)
		time.Sleep(calltracker.VoiceHang * 3 / 4)
		s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
		time.Sleep(calltracker.VoiceHang / 2)
		release()
		ct.Flush()
		synctest.Wait()
		for {
			select {
			case msg := <-sub.Channel():
				var c models.Call
				require.NoError(t, json.Unmarshal(msg, &c))
				assert.True(t, c.Active, "the call ended while a burst within its hang waited in the queue")
				continue
			default:
			}
			break
		}

		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		waitDone(t, ct, database, 1)
	})
}

// TestCopiesAfterTheTerminatorMakeNoRow sends the last bursts of a call
// again after its terminator, as a network that repeats or reorders
// packets does. The copies are no new call.
func TestCopiesAfterTheTerminatorMakeNoRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)

		s := newSender(t, ct, userA, tgID, true, 7062)
		s.voiceCall(2, false)
		time.Sleep(150 * time.Millisecond)
		term := s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		ct.Flush()
		for i, v := range []uint{4, 5} {
			p := s.packet(dmrconst.FrameVoice, v, layer2.RawBurst{})
			p.Seq = (term.Seq - 2 + uint(i)) & 0xff
			ct.Track(context.Background(), p, models.RepeaterTypeMMDVM, false)
		}
		time.Sleep(calltracker.VoiceHang + time.Second)

		calls := waitDone(t, ct, database, 1)
		assert.Zero(t, calls[0].Loss)
	})
}

// TestANewCallOnAReusedStreamIDHasItsOwnRow sends a call, and then new
// calls of the same source, destination and slot that use its stream ID
// again with sequence numbers from zero, as the stream IDs of an IPSC
// server, which count from 1 again after a restart, do. A new call is not
// a copy of the last one and does not resume it: one that starts with a
// voice header, and one that starts late, after the voice hang, each have
// their own row.
func TestANewCallOnAReusedStreamIDHasItsOwnRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)

		s := newSender(t, ct, userA, tgID, true, 7063)
		s.voiceCall(2, false)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		ct.Flush()

		s.seq = 0
		time.Sleep(100 * time.Millisecond)
		s.voiceCall(2, false)
		time.Sleep(calltracker.VoiceHang + time.Second)
		ct.Flush()

		s.seq = 0
		for v := uint(1); v <= 5; v++ {
			s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
		}
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		waitDone(t, ct, database, 3)
	})
}

// TestALateEntryOnAReusedStreamIDAfterALongGapHasItsOwnRow ends a call on
// the hang and starts a new call 14.6 s later on the same stream ID, late,
// without a voice header and with sequence numbers from zero, where a
// stream that went on through the gap would be. The new call has its own
// row: a gap in the sequence numbers of a stream is not taken as a call
// that went on.
func TestALateEntryOnAReusedStreamIDAfterALongGapHasItsOwnRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		s := newSender(t, ct, userA, tgID, true, 7099)
		s.voiceCall(2, false)
		ct.Flush()
		time.Sleep(14640 * time.Millisecond)
		s.seq = 0
		for v := uint(1); v <= 5; v++ {
			s.send(dmrconst.FrameVoice, v, layer2.RawBurst{})
		}
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		waitDone(t, ct, database, 2)
	})
}
