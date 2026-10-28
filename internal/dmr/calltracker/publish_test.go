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
	"errors"
	"fmt"
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
)

const slowPublish = 500 * time.Millisecond

type slowPubSub struct{ pubsub.PubSub }

func (p slowPubSub) Publish(topic string, message []byte) error {
	time.Sleep(slowPublish)
	return p.PubSub.Publish(topic, message) //nolint:wrapcheck
}

// TestMarksForOtherReplicasWaitOnNoPublish marks a burst of a call that
// another replica tracks as undeliverable and as converted while every
// publish takes 500 ms. The callers deliver bursts, some under a lock of
// their server, so they must not wait for the publish, and each mark must
// still reach the other replicas.
func TestMarksForOtherReplicasWaitOnNoPublish(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, kvStore := trackerDeps(t)
		ct := calltracker.NewCallTracker(database, slowPubSub{ps}, kvStore)
		t.Cleanup(ct.Stop)
		undeliverable := ps.Subscribe("calltracker:undeliverable")
		t.Cleanup(func() { _ = undeliverable.Close() })
		converted := ps.Subscribe("calltracker:converted")
		t.Cleanup(func() { _ = converted.Close() })

		p := models.Packet{StreamID: 777, Src: userA, Dst: tgID, GroupCall: true}
		start := time.Now()
		ct.Undeliverable(p, "ipsc:311304200:slot_busy")
		ct.Converted(p, "codec2", "repeater:311860")
		waited := time.Since(start)
		t.Logf("marking two bursts waited %v with %v per publish", waited, slowPublish)
		assert.Less(t, waited, 20*time.Millisecond, "a mark waited on pubsub")

		for name, sub := range map[string]pubsub.Subscription{"undeliverable": undeliverable, "converted": converted} {
			select {
			case <-sub.Channel():
			case <-time.After(5 * time.Second):
				require.Fail(t, "the mark did not reach the other replicas", name)
			}
		}
	})
}

// pausedPubSub acts as a Redis that stops while paused is set. With
// notSent set, as a stopped Redis, each publish fails at once and never
// runs; without it, as a paused Redis, each publish waits slowPublish and
// fails.
type pausedPubSub struct {
	pubsub.PubSub
	paused  atomic.Bool
	notSent atomic.Bool
	calls   atomic.Int64
}

func (p *pausedPubSub) Publish(topic string, message []byte) error {
	if !p.paused.Load() {
		return p.PubSub.Publish(topic, message) //nolint:wrapcheck
	}
	p.calls.Add(1)
	if p.notSent.Load() {
		time.Sleep(5 * time.Millisecond)
		return fmt.Errorf("dial tcp: connection refused: %w", pubsub.ErrNotSent)
	}
	time.Sleep(slowPublish)
	return errors.New("read tcp: i/o timeout")
}

// TestAPausedRedisHoldsNoCallTracking tracks a group call while each
// publish waits 500 ms and fails, as a paused Redis makes it. The worker
// that tracks the call publishes each update of the call, and must not
// wait on the publishes, or its queue fills and it drops the packets of
// every call on it.
func TestAPausedRedisHoldsNoCallTracking(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, kvStore := trackerDeps(t)
		paused := &pausedPubSub{PubSub: ps}
		ct := calltracker.NewCallTracker(database, paused, kvStore)
		t.Cleanup(ct.Stop)

		paused.paused.Store(true)
		start := time.Now()
		newSender(t, ct, userA, tgID, true, 801).voiceCall(1, true)
		ct.Flush()
		waited := time.Since(start)
		t.Logf("tracking a call of 8 bursts took %v with %v per publish, %d publishes", waited, slowPublish, paused.calls.Load())
		assert.Less(t, waited, slowPublish/2, "tracking waited on pubsub")
		assert.Zero(t, ct.Dropped(), "the tracker dropped packets")
		paused.paused.Store(false)
		time.Sleep(slowPublish)
	})
}

// TestCallUpdatesThatDidNotReachRedisAreSentAgain tracks a group call
// while each publish fails at once without reaching Redis, as when Redis
// is down. Once Redis is back, the web clients must get every update of
// the call, in order.
func TestCallUpdatesThatDidNotReachRedisAreSentAgain(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, kvStore := trackerDeps(t)
		paused := &pausedPubSub{PubSub: ps}
		ct := calltracker.NewCallTracker(database, paused, kvStore)
		t.Cleanup(ct.Stop)
		sub := ps.Subscribe("calls")
		t.Cleanup(func() { _ = sub.Close() })

		updates := func() []models.Call {
			var got []models.Call
			for {
				select {
				case msg := <-sub.Channel():
					var c models.Call
					require.NoError(t, json.Unmarshal(msg, &c))
					got = append(got, c)
					if !c.Active {
						return got
					}
				case <-time.After(10 * time.Second):
					return got
				}
			}
		}
		call := func(stream uint) {
			s := newSender(t, ct, userA, tgID, true, stream)
			s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
			for v := uint(0); v <= 5; v++ {
				time.Sleep(20 * time.Millisecond)
				frame := dmrconst.FrameVoice
				if v == 0 {
					frame = dmrconst.FrameVoiceSync
				}
				s.send(frame, v, layer2.RawBurst{})
			}
			time.Sleep(20 * time.Millisecond)
			s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
			ct.Flush()
		}

		call(802)
		want := updates()
		require.NotEmpty(t, want)

		paused.notSent.Store(true)
		paused.paused.Store(true)
		call(803)
		time.Sleep(300 * time.Millisecond)
		paused.paused.Store(false)
		got := updates()
		t.Logf("the web clients got %d of %d updates after %d failed publishes", len(got), len(want), paused.calls.Load())
		require.Len(t, got, len(want), "updates missing or twice")
		durations := make([]time.Duration, len(got))
		for i, c := range got {
			durations[i] = c.Duration
		}
		assert.IsNonDecreasing(t, durations, "the updates came out of order")
		assert.False(t, got[len(got)-1].Active, "the last update is not the end of the call")
	})
}
