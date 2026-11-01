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
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
)

func settleRig(t *testing.T) (kv.KV, func(id string) *Server) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		_ = store.Close()
	})
	return store, func(id string) *Server {
		s := &Server{
			clock: clock.Real(), kv: store, pubsub: ps, id: id,
			done: make(chan struct{}), egressOut: make(chan outMessage, outboxSize),
			markers: xsync.NewMap[string, chan struct{}](), released: make(chan struct{}, 1), handedOver: make(chan struct{}),
		}
		s.wg.Add(1)
		go s.runOutbox(s.egressOut)
		t.Cleanup(func() {
			close(s.done)
			s.wg.Wait()
		})
		return s
	}
}

func consume(s *Server, sub pubsub.Subscription) {
	s.wg.Add(1)
	go s.consumeEgress(sub)
}

func closed(ch chan struct{}) bool {
	synctest.Wait()
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// TestABarrierWaitsForTheHolderOfTheLease hands a marker of a Barrier from
// a replica without the egress lease to the holder, which reads the egress
// topic late, as a starved one does. The Barrier must end only once the
// holder took the marker, and so sent the packets before it.
func TestABarrierWaitsForTheHolderOfTheLease(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, replica := settleRig(t)
		holder, other := replica("b"), replica("a")
		ctx := context.Background()
		require.True(t, holder.holdLease(ctx))
		holder.role.Store(roleLeader)
		late := holder.pubsub.Subscribe(egressTopic)
		consume(other, other.pubsub.Subscribe(egressTopic))

		done := make(chan struct{})
		other.settleEgress(ctx, done)
		require.False(t, closed(done), "the Barrier ended before the holder took the marker")
		consume(holder, late)
		require.True(t, closed(done), "the Barrier did not end once the holder took the marker")
	})
}

// TestABarrierWithAnOlderHolderEndsOnTheTopic hands a marker of a Barrier
// to a holder of an older DMRHub, whose lease has no word that it answers
// markers. The Barrier must end once the marker is on the egress topic,
// with no wait for an answer that never comes.
func TestABarrierWithAnOlderHolderEndsOnTheTopic(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		store, replica := settleRig(t)
		other := replica("a")
		ctx := context.Background()
		require.NoError(t, store.Set(ctx, egressLeaseKey, []byte("old")))
		on := other.pubsub.Subscribe(egressTopic)
		t.Cleanup(func() { _ = on.Close() })

		done := make(chan struct{})
		other.settleEgress(ctx, done)
		require.True(t, closed(done), "the Barrier waited for an answer from an older holder")
		require.Contains(t, string(<-on.Channel()), `"kind":"marker"`, "the marker is not on the topic")
	})
}
