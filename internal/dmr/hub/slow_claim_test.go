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
	"cmp"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const slowKVCall = 500 * time.Millisecond

type slowKV struct {
	kv.KV
	on    atomic.Bool
	delay time.Duration
}

func (s *slowKV) wait() {
	if s.on.Load() {
		time.Sleep(cmp.Or(s.delay, slowKVCall))
	}
}

func (s *slowKV) Has(ctx context.Context, key string) (bool, error) {
	s.wait()
	return s.KV.Has(ctx, key) //nolint:wrapcheck
}

func (s *slowKV) Get(ctx context.Context, key string) ([]byte, error) {
	s.wait()
	return s.KV.Get(ctx, key) //nolint:wrapcheck
}

func (s *slowKV) Set(ctx context.Context, key string, value []byte) error {
	s.wait()
	return s.KV.Set(ctx, key, value) //nolint:wrapcheck
}

func (s *slowKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	s.wait()
	return s.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

func (s *slowKV) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	s.wait()
	return s.KV.SetNX(ctx, key, value, ttl) //nolint:wrapcheck
}

// TestSlowClaimStoreHoldsNoReadLoop starts a stream while every call to the
// KV store takes 500 ms and ignores its context, as a hung Redis does with
// the default client options. The read loop that routes it, and another
// stream on the same read loop, must not wait on the claim of the stream;
// the new stream waits at most StreamClaimWait and keeps its order, and the
// ongoing stream does not wait when it renews its claim. Each burst is
// timed from when it was due, as a socket queues it while the read loop
// is busy.
func TestSlowClaimStoreHoldsNoReadLoop(t *testing.T) {
	t.Parallel()
	slow := &slowKV{}
	h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
		slow.KV = k
		return slow
	})

	seedUser(t, database, testSrcID, "CALLER")
	seedUser(t, database, 1000002, "OTHER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedRepeater(t, database, 100002, 1000002)
	seedTalkgroup(t, database, 1, "TG1")
	var rpt2 models.Repeater
	require.NoError(t, database.First(&rpt2, 100002).Error)
	require.NoError(t, database.Model(&rpt2).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))
	require.NoError(t, database.Model(&rpt2).Association("TS2StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))

	arrivals := collect(map[string]*hub.ServerHandle{
		"mmdvm": h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater}),
	})
	h.SetStreamClaimTiming(hub.StreamClaimHold, 150*time.Millisecond)
	h.ActivateRepeater(context.Background(), testRepeaterID)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	burst := func(stream uint, slot bool, src uint, seq int) models.Packet {
		p := makeVoicePacket(1, stream, true, slot)
		p.Src = src
		p.Seq = uint(seq)
		return p
	}
	const ongoing, started = uint(70101), uint(70102)
	worstDelay(t, arrivals, 70100, sendStream(h, 1, 70100, true, 4), "mmdvm:100002")
	h.RoutePacket(context.Background(), burst(ongoing, false, testSrcID, 0), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)

	slow.on.Store(true)
	defer slow.on.Store(false)
	const bursts = 8
	sent := map[uint][]time.Time{ongoing: make([]time.Time, bursts), started: make([]time.Time, bursts)}
	var worstCall time.Duration
	start := time.Now()
	for i := range bursts {
		time.Sleep(time.Until(start.Add(time.Duration(i) * burstPeriod)))
		for _, p := range []models.Packet{burst(ongoing, false, testSrcID, i), burst(started, true, 1000002, i)} {
			sent[p.StreamID][i] = start.Add(time.Duration(i) * burstPeriod)
			called := time.Now()
			h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
			worstCall = max(worstCall, time.Since(called))
		}
	}

	worst := map[uint]time.Duration{}
	seqs := map[uint][]uint{}
	deadline := time.After(bursts*burstPeriod + 30*time.Second)
	for len(seqs[ongoing]) < bursts || len(seqs[started]) < bursts {
		select {
		case a := <-arrivals:
			s := a.rp.Packet.StreamID
			if s != ongoing && s != started {
				continue
			}
			seqs[s] = append(seqs[s], a.rp.Packet.Seq)
			worst[s] = max(worst[s], a.at.Sub(sent[s][a.rp.Packet.Seq]))
		case <-deadline:
			t.Fatalf("deliveries %v, want %d of each stream", seqs, bursts)
		}
	}
	t.Logf("worst RoutePacket call %v, worst delay of the ongoing stream %v, of the new stream %v, with %v per KV call", worstCall, worst[ongoing], worst[started], slowKVCall)
	assert.Less(t, worstCall, 20*time.Millisecond, "the read loop waited on the KV store")
	assert.Less(t, worst[ongoing], 50*time.Millisecond, "the ongoing stream waited on the KV store")
	assert.Less(t, worst[started], hub.StreamClaimWait+50*time.Millisecond, "the new stream waited on the KV store for too long")
	assert.Equal(t, []uint{0, 1, 2, 3, 4, 5, 6, 7}, seqs[started], "the held bursts of the new stream kept their order")
}

// TestAStreamStartedOnTwoReplicasAtOnceHasOneSource starts the same stream
// from two sources on two replicas at the same time, over a KV store that
// answers in 10 ms. Exactly one source may own it.
func TestAStreamStartedOnTwoReplicasAtOnceHasOneSource(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	inner, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	shared := &slowKV{KV: inner, delay: 10 * time.Millisecond}
	shared.on.Store(true)
	one, two := hub.NewHub(nil, shared, nil, nil), hub.NewHub(nil, shared, nil, nil)
	t.Cleanup(func() {
		one.Stop(context.Background())
		two.Stop(context.Background())
		_ = shared.Close()
	})

	for stream := uint(1); stream <= 20; stream++ {
		var wg sync.WaitGroup
		var admitted atomic.Int32
		for i, h := range []*hub.Hub{one, two} {
			wg.Go(func() {
				p := makeVoicePacket(1, stream, true, false)
				p.Repeater = 100001 + uint(i)
				if h.AdmitStream(t.Context(), p, models.RepeaterTypeMMDVM) {
					admitted.Add(1)
				}
			})
		}
		wg.Wait()
		assert.Equal(t, int32(1), admitted.Load(), "stream %d", stream)
	}
}
