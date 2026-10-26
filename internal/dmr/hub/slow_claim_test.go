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
	"fmt"
	"strings"
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
	hang  chan struct{}
	mu    sync.Mutex
	setNX []string
}

func (s *slowKV) wait() {
	if !s.on.Load() {
		return
	}
	if s.hang != nil {
		<-s.hang
		return
	}
	time.Sleep(cmp.Or(s.delay, slowKVCall))
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
	if s.on.Load() {
		s.mu.Lock()
		s.setNX = append(s.setNX, key)
		s.mu.Unlock()
	}
	s.wait()
	return s.KV.SetNX(ctx, key, value, ttl) //nolint:wrapcheck
}

// TestSlowClaimStoreHoldsNoReadLoop starts a stream while every call to the
// KV store hangs and ignores its context, as a hung Redis does with the
// default client options. The read loop that routes it, and another stream
// on the same read loop, must not wait on the claim of the stream: every
// call to route a burst returns and every burst of both streams arrives
// while the KV store still hangs. A call that waited on the claim would
// take at least StreamClaimWait, as only the hub gives up on a hung KV
// store. The bursts of the new stream share one check of its claim, which
// the hub gives up after StreamClaimWait, and keep their order; the
// ongoing stream renews its claim without waiting.
func TestSlowClaimStoreHoldsNoReadLoop(t *testing.T) {
	t.Parallel()
	slow := &slowKV{hang: make(chan struct{})}
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
	worstDelay(t, arrivals, ongoing, []time.Time{time.Now()}, "mmdvm:100002")
	time.Sleep(200 * time.Millisecond)

	slow.on.Store(true)
	defer close(slow.hang)
	const bursts = 8
	routed := make(chan time.Duration, 1)
	go func() {
		var worstCall time.Duration
		start := time.Now()
		for i := range bursts {
			time.Sleep(time.Until(start.Add(time.Duration(i) * burstPeriod)))
			for _, p := range []models.Packet{burst(ongoing, false, testSrcID, i), burst(started, true, 1000002, i)} {
				called := time.Now()
				h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
				worstCall = max(worstCall, time.Since(called))
			}
		}
		routed <- worstCall
	}()

	seqs := map[uint][]uint{}
	deadline := time.After(bursts*burstPeriod + 30*time.Second)
	for len(seqs[ongoing]) < bursts || len(seqs[started]) < bursts {
		select {
		case a := <-arrivals:
			s := a.rp.Packet.StreamID
			if s == ongoing || s == started {
				seqs[s] = append(seqs[s], a.rp.Packet.Seq)
			}
		case <-deadline:
			t.Fatalf("deliveries %v while the KV store hangs, want %d of each stream", seqs, bursts)
		}
	}
	select {
	case worstCall := <-routed:
		assert.Less(t, worstCall, hub.StreamClaimWait, "the read loop waited on the claim of a stream")
	case <-deadline:
		t.Fatal("the read loop waited on the KV store")
	}
	checks := 0
	slow.mu.Lock()
	for _, key := range slow.setNX {
		if strings.Contains(key, fmt.Sprintf(":%d:", started)) {
			checks++
		}
	}
	slow.mu.Unlock()
	assert.Equal(t, 1, checks, "the held bursts of the new stream shared one check of its claim")
	assert.Equal(t, []uint{0, 1, 2, 3, 4, 5, 6, 7}, seqs[ongoing], "the ongoing stream kept its order")
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
