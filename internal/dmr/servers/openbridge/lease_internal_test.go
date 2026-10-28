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
package openbridge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEgressLeaseMovesAfterACrash lets the holder of the egress lease stop
// renewing it without giving it up, as a replica that dies does. The other
// replica must take it within EgressLeaseTTL plus one renewal.
func TestEgressLeaseMovesAfterACrash(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	a := &Server{clock: clock.Real(), kv: store, id: "a"}
	b := &Server{clock: clock.Real(), kv: store, id: "b"}

	ctx := context.Background()
	require.True(t, a.holdLease(ctx))
	require.False(t, b.holdLease(ctx), "one replica holds the lease")
	require.True(t, a.holdLease(ctx), "the holder renews it")

	crash := time.Now()
	for !b.holdLease(ctx) {
		require.Less(t, time.Since(crash), 2*EgressLeaseTTL, "the lease never moved")
		time.Sleep(egressLeaseRenew)
	}
	gap := time.Since(crash)
	t.Logf("the lease moved after %s", gap.Round(time.Millisecond))
	assert.LessOrEqual(t, gap, EgressLeaseTTL+egressLeaseRenew+200*time.Millisecond)
	assert.False(t, a.holdLease(ctx), "the old holder does not get it back")
}

// TestAHolderThatGaveTheLeaseUpStopsAtTheTakenMessage gives a holder's
// lease up and lets another replica announce that it took it. The topic
// has one order, and the new holder sends each packet that comes after its
// taken message, so the old one must send none of them, or the peers get
// them twice.
func TestAHolderThatGaveTheLeaseUpStopsAtTheTakenMessage(t *testing.T) {
	t.Parallel()
	s := &Server{clock: clock.Real(), id: "a", handedOver: make(chan struct{}), released: make(chan struct{}, 1)}
	s.role.Store(roleReleasing)
	require.True(t, s.sends(), "a holder that gave the lease up sends until it is taken")
	s.leaseMessage(egressJob{Kind: kindTaken, Replica: "b"})
	select {
	case <-s.handedOver:
	default:
		t.Fatal("the holder was not told that the lease was taken")
	}
	assert.False(t, s.sends(), "the old holder sends the packets after the taken message too")
}

// TestAHolderThatGaveTheLeaseUpSendsUntilTheTakenMessage gives a holder's
// lease up, and another replica takes the lease in the KV store but its
// taken message comes late. The packets on the egress topic before that
// message are the old holder's to send, so it must keep sending until the
// message comes, or they are lost.
func TestAHolderThatGaveTheLeaseUpSendsUntilTheTakenMessage(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ctx := context.Background()
	store, err := kv.MakeKV(ctx, &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ps, err := pubsub.MakePubSub(ctx, &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	s := &Server{clock: clock.Real(), kv: store, pubsub: ps, id: "a", handedOver: make(chan struct{}), released: make(chan struct{}, 1)}
	require.True(t, s.holdLease(ctx))
	s.role.Store(roleLeader)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handOver(ctx)
	}()
	b := &Server{clock: clock.Real(), kv: store, id: "b"}
	require.Eventually(t, func() bool { return b.holdLease(ctx) }, time.Second, 10*time.Millisecond)
	select {
	case <-done:
		t.Fatal("the old holder stopped before the taken message of the new one")
	case <-time.After(5 * handoverCheck):
	}
	assert.True(t, s.sends(), "the old holder stopped sending before the taken message")
	s.leaseMessage(egressJob{Kind: kindTaken, Replica: "b"})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the old holder did not stop at the taken message")
	}
	assert.False(t, s.sends())
}

// TestAHolderThatGaveTheLeaseUpStopsAfterAnOldTaker gives a holder's lease
// up to a replica of an older DMRHub, which takes it without a taken
// message and sends at once. The old holder must stop within takeWait.
func TestAHolderThatGaveTheLeaseUpStopsAfterAnOldTaker(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ctx := context.Background()
	store, err := kv.MakeKV(ctx, &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ps, err := pubsub.MakePubSub(ctx, &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	s := &Server{clock: clock.Real(), kv: store, pubsub: ps, id: "a", handedOver: make(chan struct{}), released: make(chan struct{}, 1)}
	require.True(t, s.holdLease(ctx))
	s.role.Store(roleLeader)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handOver(ctx)
	}()
	b := &Server{clock: clock.Real(), kv: store, id: "b"}
	require.Eventually(t, func() bool { return b.holdLease(ctx) }, time.Second, 10*time.Millisecond)
	took := time.Now()
	select {
	case <-done:
	case <-time.After(handoverWait):
		t.Fatal("the old holder did not stop after an old replica took the lease")
	}
	assert.Less(t, time.Since(took), takeWait+2*handoverCheck+200*time.Millisecond)
}

// TestTheOutboxPublishesWhatWaitsAtTheStop stops a replica with egress
// still in its outbox, as when it hands packets to the holder of the lease
// up to its stop. The holder sends them, so they must still go out on the
// egress topic.
func TestTheOutboxPublishesWhatWaitsAtTheStop(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	sub := ps.Subscribe(egressTopic)
	t.Cleanup(func() { _ = sub.Close() })
	s := &Server{clock: clock.Real(), pubsub: ps, done: make(chan struct{})}
	out := make(chan outMessage, 4)
	for i := range 3 {
		out <- outMessage{topic: egressTopic, msg: []byte{byte(i)}}
	}
	close(s.done)
	s.wg.Add(1)
	s.runOutbox(out)
	for i := range 3 {
		select {
		case m := <-sub.Channel():
			assert.Equal(t, []byte{byte(i)}, m)
		case <-time.After(time.Second):
			t.Fatalf("message %d of the outbox was not published", i)
		}
	}
}

var errPausedRedis = errors.New("read tcp: i/o timeout")

// pausedKV acts as a Redis that stops answering while paused: each call
// fails, as a call to a paused Redis times out.
type pausedKV struct {
	kv.KV
	paused atomic.Bool
}

func (p *pausedKV) Get(ctx context.Context, key string) ([]byte, error) {
	if p.paused.Load() {
		return nil, errPausedRedis
	}
	return p.KV.Get(ctx, key) //nolint:wrapcheck
}

func (p *pausedKV) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	if p.paused.Load() {
		return false, errPausedRedis
	}
	return p.KV.SetNX(ctx, key, value, ttl) //nolint:wrapcheck
}

func (p *pausedKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if p.paused.Load() {
		return errPausedRedis
	}
	return p.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

// TestTheHolderSendsUntilItsLeaseRunsOutInARedisOutage pauses Redis while
// a replica holds the egress lease. No other replica can take the lease
// before it expires, so the holder must keep sending until EgressLeaseTTL
// after its last renewal, and then stop.
func TestTheHolderSendsUntilItsLeaseRunsOutInARedisOutage(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ctx := context.Background()
	store, err := kv.MakeKV(ctx, &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	paused := &pausedKV{KV: store}
	s := &Server{clock: clock.Real(), kv: paused, id: "a"}
	s.renewLease(ctx)
	require.True(t, s.IsEgressLeader())

	renewed := time.Now()
	s.renewLease(ctx)
	require.True(t, s.IsEgressLeader())
	paused.paused.Store(true)
	for time.Since(renewed) < EgressLeaseTTL-egressLeaseRenew {
		s.renewLease(ctx)
		require.True(t, s.IsEgressLeader(), "the holder stopped %s after its last renewal, before its lease ran out", time.Since(renewed).Round(time.Millisecond))
		time.Sleep(egressLeaseRenew / 4)
	}
	time.Sleep(time.Until(renewed.Add(EgressLeaseTTL)))
	s.renewLease(ctx)
	assert.False(t, s.IsEgressLeader(), "the holder still sends after its lease ran out")

	paused.paused.Store(false)
	s.renewLease(ctx)
	assert.NotEqual(t, roleFollower, s.role.Load(), "the holder takes the lease again once Redis is back")
}
