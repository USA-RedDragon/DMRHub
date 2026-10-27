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

package hub_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pausedKV acts as a Redis that stops while paused, for the keys that
// start with prefix: each call waits until it goes on, and fails.
type pausedKV struct {
	kv.KV
	prefix string
	mu     sync.Mutex
	gate   chan struct{}
	calls  atomic.Int64
	held   atomic.Int64
}

var errPausedKV = errors.New("read tcp: i/o timeout")

// pause makes the calls on the keys from now on wait for resume.
func (p *pausedKV) pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gate == nil {
		p.gate = make(chan struct{})
	}
}

// resume lets the waiting calls fail, and pauses no more.
func (p *pausedKV) resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gate != nil {
		close(p.gate)
		p.gate = nil
	}
}

func (p *pausedKV) stalls(key string) bool {
	if !strings.HasPrefix(key, p.prefix) {
		return false
	}
	p.mu.Lock()
	gate := p.gate
	p.mu.Unlock()
	if gate == nil {
		return false
	}
	p.calls.Add(1)
	p.held.Add(1)
	<-gate
	p.held.Add(-1)
	return true
}

func (p *pausedKV) Get(ctx context.Context, key string) ([]byte, error) {
	if p.stalls(key) {
		return nil, errPausedKV
	}
	return p.KV.Get(ctx, key) //nolint:wrapcheck
}

func (p *pausedKV) Set(ctx context.Context, key string, value []byte) error {
	if p.stalls(key) {
		return errPausedKV
	}
	return p.KV.Set(ctx, key, value) //nolint:wrapcheck
}

func (p *pausedKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if p.stalls(key) {
		return errPausedKV
	}
	return p.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

// TestAPausedRedisLosesNoSighting hears 300 radios on a trusted repeater
// while every call on the records of where they were heard waits and
// fails, as a paused Redis makes it. The worker that takes the sightings
// must not wait on Redis, or its queue fills and it drops sightings: the
// record of each radio must wait on Redis while Redis is paused, and be
// written once Redis goes on.
func TestAPausedRedisLosesNoSighting(t *testing.T) {
	t.Parallel()
	const radios, first, batch = 300, 1100000, 50
	paused := &pausedKV{prefix: "dmrhub:lastheard:11"}
	h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
		paused.KV = k
		return paused
	})
	t.Cleanup(paused.resume)
	t.Cleanup(h.HoldLookupWaits())
	seedUser(t, database, 1000001, "OWNER")
	seedRepeater(t, database, 100003, 1000001)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100003).Error)
	for i := range radios {
		seedUser(t, database, first+uint(i), "R"+strconv.Itoa(i))
	}
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	h.ActivateRepeater(context.Background(), 100003)
	h.LiftSlotRate(true)
	time.Sleep(100 * time.Millisecond)

	paused.pause()
	for i := range radios {
		h.RoutePacket(context.Background(), dataHeader(first+uint(i), 1, 100003, 9000+uint(i)), models.RepeaterTypeMMDVM)
		if i%batch == batch-1 {
			require.Eventually(t, func() bool { return paused.held.Load() == int64(i+1) }, time.Minute, 10*time.Millisecond,
				"the records of the first %d radios do not all wait on the paused Redis", i+1)
		}
	}
	paused.resume()
	missing := func() []uint {
		var out []uint
		for i := range radios {
			if h.HeardAt(context.Background(), first+uint(i)) != 100003 {
				out = append(out, first+uint(i))
			}
		}
		return out
	}
	assert.Eventually(t, func() bool { return len(missing()) == 0 }, time.Minute, 100*time.Millisecond, "where some radios were heard is not stored")
	t.Logf("%d of %d records missing after %d paused KV calls", len(missing()), radios, paused.calls.Load())
}

// TestAPausedRedisHoldsNoCodecChange announces the change of the codec of
// a repeater while each publish waits and fails, as a paused Redis makes
// it, and then while each publish fails at once without reaching Redis,
// as a stopped Redis makes it. The announcement must not wait on Redis,
// since the worker that records the codecs heard on the timeslots makes
// it: it must return while Redis still holds or fails its publish. The
// other replicas must get each one once Redis is back.
func TestAPausedRedisHoldsNoCodecChange(t *testing.T) {
	t.Parallel()
	stalled := &stalledPubSub{stalls: func(topic string) bool { return topic == "hub:repeaters:codec" }}
	h, database, ps := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub {
		stalled.PubSub = ps
		return stalled
	}, func(k kv.KV) kv.KV { return k })
	t.Cleanup(stalled.resume)
	seedUser(t, database, 1000001, "OWNER")
	seedRepeater(t, database, 100003, 1000001)
	sub := ps.Subscribe("hub:repeaters:codec")
	t.Cleanup(func() { _ = sub.Close() })

	for _, mode := range []stallMode{stallTimeout, stallNotSent} {
		before := stalled.calls.Load()
		stalled.stall(mode)
		announced := make(chan struct{})
		go func() {
			h.CodecChanged(100003)
			close(announced)
		}()
		select {
		case <-announced:
		case <-time.After(time.Minute):
			require.Fail(t, "the announcement waited on pubsub", "stall mode %d", mode)
		}
		require.Eventually(t, func() bool { return stalled.calls.Load() > before }, time.Minute, 10*time.Millisecond,
			"the announcement made no publish for pubsub to stall in stall mode %d", mode)
		stalled.resume()
		select {
		case msg := <-sub.Channel():
			assert.True(t, strings.HasSuffix(string(msg), ":100003"), "the announcement names the repeater: %s", msg)
		case <-time.After(time.Minute):
			assert.Fail(t, "the other replicas did not get the announcement", "stall mode %d", mode)
		}
		select {
		case msg := <-sub.Channel():
			assert.Fail(t, "the other replicas got the announcement twice", "%s", msg)
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// TestOwnsRepeaterWaitsOnAPausedRedisNoLongerThanALookup asks whether the
// hub owns a repeater while each read of the owner of a repeater waits a
// second and fails, as a paused Redis makes it. The MMDVM server asks it
// before it sends to a repeater whose session it resumes, so it must wait
// no longer than a database lookup does, and then answer from what this
// hub knows: it owns a repeater that it claimed, unless another replica
// claimed it after that.
func TestOwnsRepeaterWaitsOnAPausedRedisNoLongerThanALookup(t *testing.T) {
	t.Parallel()
	paused := &pausedKV{prefix: "dmrhub:peer_owner:"}
	h, database, ps := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
		paused.KV = k
		return paused
	})
	require.NoError(t, h.SetInstanceID("replica-a"))
	seedUser(t, database, 1000001, "OWNER")
	seedRepeater(t, database, 100003, 1000001)
	seedRepeater(t, database, 100004, 1000001)
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	h.ActivateRepeater(context.Background(), 100003)
	h.ActivateRepeater(context.Background(), 100004)
	time.Sleep(100 * time.Millisecond)
	require.True(t, h.OwnsRepeater(context.Background(), 100003))
	require.True(t, h.OwnsRepeater(context.Background(), 100004))

	require.NoError(t, ps.Publish("hub:claimed", fmt.Appendf(nil, "100004 replica-b %d", time.Now().UnixNano())))
	time.Sleep(100 * time.Millisecond)

	t.Cleanup(paused.resume)
	paused.pause()
	for id, want := range map[uint]bool{100003: true, 100004: false} {
		start := time.Now()
		owns := h.OwnsRepeater(context.Background(), id)
		waited := time.Since(start)
		t.Logf("OwnsRepeater(%d) = %v after %v while the KV store holds its calls", id, owns, waited)
		assert.Less(t, waited, lookup.Wait+200*time.Millisecond, "OwnsRepeater waited on a paused KV store")
		assert.Equal(t, want, owns, "OwnsRepeater(%d) with a paused KV store", id)
	}
}
