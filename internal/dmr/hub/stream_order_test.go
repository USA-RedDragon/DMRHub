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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const orderPeerID = uint(3100100)

// heldPublish holds the publish of the terminator of stream until a burst
// of next is published, or until wait passes.
type heldPublish struct {
	pubsub.PubSub
	stream, next uint
	wait         time.Duration
	once         sync.Once
	nextSeen     chan struct{}
}

func (p *heldPublish) Publish(topic string, message []byte) error {
	var raw models.RawDMRPacket
	if _, err := raw.UnmarshalMsg(message); err == nil {
		if pkt, ok := models.UnpackPacket(raw.Data); ok {
			switch pkt.StreamID {
			case p.next:
				p.once.Do(func() { close(p.nextSeen) })
			case p.stream:
				if pkt.FrameType == makeVoiceTermPacket(0, 0, false, false).FrameType {
					select {
					case <-p.nextSeen:
					case <-time.After(p.wait):
					}
				}
			}
		}
	}
	return p.PubSub.Publish(topic, message) //nolint:wrapcheck
}

// orderHub seeds a source repeater testRepeaterID and a target repeater
// 100002 with TGs 1 and 2 static on TS1, and an egress peer.
func orderHub(t *testing.T, wrap func(pubsub.PubSub) pubsub.PubSub, wrapKV func(kv.KV) kv.KV) (*hub.Hub, <-chan arrival) {
	t.Helper()
	h, database, _ := makeTestHubWrappingKV(t, wrap, wrapKV)
	seedUser(t, database, testSrcID, "CALLER")
	seedUser(t, database, 1000002, "OTHER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedRepeater(t, database, 100002, 1000002)
	seedTalkgroup(t, database, 1, "TG1")
	seedTalkgroup(t, database, 2, "TG2")
	var rpt2 models.Repeater
	require.NoError(t, database.First(&rpt2, 100002).Error)
	require.NoError(t, database.Model(&rpt2).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}, &models.Talkgroup{ID: 2}))
	require.NoError(t, database.Create(&models.Peer{ID: orderPeerID, OwnerID: testSrcID, Egress: true}).Error)
	require.NoError(t, database.Create(&models.PeerRule{PeerID: orderPeerID, SubjectIDMin: 1, SubjectIDMax: 9999999}).Error)
	arrivals := collect(map[string]*hub.ServerHandle{
		"mmdvm": h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater}),
		"peers": h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer}),
	})
	h.ActivateRepeater(context.Background(), testRepeaterID)
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)
	return h, arrivals
}

type delivered struct{ stream, seq uint }

// receiveOrder collects the bursts of streams that each target gets, until
// every target has want of them.
func receiveOrder(t *testing.T, arrivals <-chan arrival, streams map[uint]bool, want int, targets ...string) map[string][]delivered {
	t.Helper()
	got := map[string][]delivered{}
	deadline := time.After(30 * time.Second)
	for {
		done := true
		for _, target := range targets {
			done = done && len(got[target]) >= want
		}
		if done {
			return got
		}
		select {
		case a := <-arrivals:
			if !streams[a.rp.Packet.StreamID] {
				continue
			}
			target := a.server + ":" + strconv.FormatUint(uint64(a.rp.RepeaterID), 10)
			got[target] = append(got[target], delivered{a.rp.Packet.StreamID, a.rp.Packet.Seq})
		case <-deadline:
			t.Fatalf("deliveries %v, want %d on each of %v", got, want, targets)
		}
	}
}

func orderOf(sent []models.Packet) []delivered {
	out := make([]delivered, len(sent))
	for i, p := range sent {
		out[i] = delivered{p.StreamID, p.Seq}
	}
	return out
}

// TestTheNextStreamOfASourceDoesNotOvertakeItsLastOne ends a stream and
// starts the next on the same repeater slot at once, and holds the publish
// of the terminator of the first until the second is published, or for
// 300 ms. Each target must get all of the first stream before any of the
// second.
func TestTheNextStreamOfASourceDoesNotOvertakeItsLastOne(t *testing.T) {
	t.Parallel()
	const first, second = uint(71001), uint(71002)
	held := &heldPublish{stream: first, next: second, wait: 300 * time.Millisecond, nextSeen: make(chan struct{})}
	h, arrivals := orderHub(t, func(ps pubsub.PubSub) pubsub.PubSub {
		held.PubSub = ps
		return held
	}, func(k kv.KV) kv.KV { return k })

	sent := make([]models.Packet, 0, 7)
	for i := range 3 {
		p := makeVoicePacket(1, first, true, false)
		p.Seq = uint(i)
		sent = append(sent, p)
	}
	term := makeVoiceTermPacket(1, first, true, false)
	term.Seq = 3
	sent = append(sent, term)
	for i := range 3 {
		p := makeVoicePacket(1, second, true, false)
		p.Seq = uint(i)
		sent = append(sent, p)
	}
	for _, p := range sent {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}

	got := receiveOrder(t, arrivals, map[uint]bool{first: true, second: true}, len(sent), "mmdvm:100002", "peers:3100100")
	for target, order := range got {
		assert.Equal(t, orderOf(sent), order, "the order of the bursts at %s", target)
	}
}

// TestBackToBackStreamsOfASourceKeepTheirOrder sends many short streams
// back to back from one repeater slot. Each target must get them in the
// order they came.
func TestBackToBackStreamsOfASourceKeepTheirOrder(t *testing.T) {
	t.Parallel()
	h, arrivals := orderHub(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV { return k })

	streams := map[uint]bool{}
	var sent []models.Packet
	for s := uint(72000); s < 72030; s++ {
		streams[s] = true
		p := makeVoicePacket(1, s, true, false)
		term := makeVoiceTermPacket(1, s, true, false)
		term.Seq = 1
		sent = append(sent, p, term)
	}
	for _, p := range sent {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}

	got := receiveOrder(t, arrivals, streams, len(sent), "mmdvm:100002", "peers:3100100")
	for target, order := range got {
		assert.Equal(t, orderOf(sent), order, "the order of the bursts at %s", target)
	}
}

// claimSlowFor makes the KV calls for the stream claims of streams take
// slowKVCall.
type claimSlowFor struct {
	kv.KV
	streams []uint
}

func (c *claimSlowFor) slow(key string) {
	for _, s := range c.streams {
		if strings.HasPrefix(key, "dmrhub:stream_claim:"+strconv.FormatUint(uint64(s), 10)+":") {
			time.Sleep(slowKVCall)
			return
		}
	}
}

func (c *claimSlowFor) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	c.slow(key)
	return c.KV.SetNX(ctx, key, value, ttl) //nolint:wrapcheck
}

func (c *claimSlowFor) Get(ctx context.Context, key string) ([]byte, error) {
	c.slow(key)
	return c.KV.Get(ctx, key) //nolint:wrapcheck
}

func (c *claimSlowFor) Expire(ctx context.Context, key string, ttl time.Duration) error {
	c.slow(key)
	return c.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

// claimHungFor holds the KV calls for the stream claims of streams until
// their context ends, as a KV store that does not answer does.
type claimHungFor struct {
	kv.KV
	streams []uint
}

func (c *claimHungFor) hung(ctx context.Context, key string) bool {
	for _, s := range c.streams {
		if strings.HasPrefix(key, "dmrhub:stream_claim:"+strconv.FormatUint(uint64(s), 10)+":") {
			<-ctx.Done()
			return true
		}
	}
	return false
}

func (c *claimHungFor) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	if c.hung(ctx, key) {
		return false, ctx.Err() //nolint:wrapcheck
	}
	return c.KV.SetNX(ctx, key, value, ttl) //nolint:wrapcheck
}

func (c *claimHungFor) Get(ctx context.Context, key string) ([]byte, error) {
	if c.hung(ctx, key) {
		return nil, ctx.Err() //nolint:wrapcheck
	}
	return c.KV.Get(ctx, key) //nolint:wrapcheck
}

func (c *claimHungFor) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if c.hung(ctx, key) {
		return ctx.Err() //nolint:wrapcheck
	}
	return c.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

// TestTheNextStreamWaitsForTheClaimOfTheLastOne starts a stream whose
// claim the KV store does not answer, ends it, and starts the next on the
// same repeater slot, whose claim is answered at once, and holds the wait
// of the hub for the claim. The read loop must not wait, a stream of
// another slot must pass, and neither stream of the held slot may pass.
// When the wait ends, both streams must pass in order.
func TestTheNextStreamWaitsForTheClaimOfTheLastOne(t *testing.T) {
	t.Parallel()
	const first, second, other = uint(73001), uint(73002), uint(73003)
	h, arrivals := orderHub(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
		return &claimHungFor{KV: k, streams: []uint{first}}
	})
	endWait := h.HoldStreamClaimWaits()
	t.Cleanup(endWait)

	sent := make([]models.Packet, 0, 5)
	p := makeVoicePacket(1, first, true, false)
	sent = append(sent, p)
	term := makeVoiceTermPacket(1, first, true, false)
	term.Seq = 1
	sent = append(sent, term)
	for i := range 3 {
		p := makeVoicePacket(1, second, true, false)
		p.Seq = uint(i)
		sent = append(sent, p)
	}
	otherSlot := makeVoicePacket(1, other, true, true)
	otherSlot.Src = 1000002
	routed := make(chan struct{})
	go func() {
		defer close(routed)
		for _, p := range append(sent, otherSlot) {
			h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		}
	}()

	var got []delivered
	otherSeen := false
	deadline := time.After(30 * time.Second)
	receive := func(done func() bool, what string) {
		for !done() {
			select {
			case <-routed:
				routed = nil
			case a := <-arrivals:
				if a.server != "mmdvm" || a.rp.RepeaterID != 100002 {
					continue
				}
				switch a.rp.Packet.StreamID {
				case other:
					otherSeen = true
				case first, second:
					got = append(got, delivered{a.rp.Packet.StreamID, a.rp.Packet.Seq})
				}
			case <-deadline:
				t.Fatalf("%s: read loop done %v, other slot passed %v, deliveries %v", what, routed == nil, otherSeen, got)
			}
		}
	}
	receive(func() bool { return routed == nil && otherSeen }, "the read loop waited on the KV store, or another slot waited on the claim")
	assert.Empty(t, got, "a stream passed before the claim of the last one was answered")
	endWait()
	receive(func() bool { return len(got) >= len(sent) }, "the streams did not pass when the wait for the claim ended")
	assert.Equal(t, orderOf(sent), got, "the next stream overtook the last one")
}

// TestTheClaimsOfNewStreamsOfASourceAreCheckedAtOnce starts three short
// streams back to back on one repeater slot, whose claims the KV store
// answers in 500 ms each. They must keep their order, and the last burst
// must not wait for the checks one after another.
func TestTheClaimsOfNewStreamsOfASourceAreCheckedAtOnce(t *testing.T) {
	t.Parallel()
	streams := []uint{74001, 74002, 74003}
	h, arrivals := orderHub(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
		return &claimSlowFor{KV: k, streams: streams}
	})

	sent := make([]models.Packet, 0, 2*len(streams))
	for _, s := range streams {
		p := makeVoicePacket(1, s, true, false)
		term := makeVoiceTermPacket(1, s, true, false)
		term.Seq = 1
		sent = append(sent, p, term)
	}
	start := time.Now()
	for _, p := range sent {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}

	var got []delivered
	var last time.Duration
	deadline := time.After(30 * time.Second)
	for len(got) < len(sent) {
		select {
		case a := <-arrivals:
			if a.server != "mmdvm" || a.rp.RepeaterID != 100002 {
				continue
			}
			got = append(got, delivered{a.rp.Packet.StreamID, a.rp.Packet.Seq})
			last = a.at.Sub(start)
		case <-deadline:
			t.Fatalf("deliveries %v", got)
		}
	}
	t.Logf("last burst at %v", last)
	assert.Equal(t, orderOf(sent), got)
	assert.Less(t, last, hub.StreamClaimWait+100*time.Millisecond, "the checks of the claims ran one after another")
}

// TestASlowClaimOfAPeerDelaysNoOtherTalkgroupOfIt starts a call of a peer
// to TG 1 whose claim the KV store answers in 500 ms, and at once a call of
// the same peer to TG 2. The call to TG 2 must not wait for the claim of
// the call to TG 1, and each must arrive in order.
func TestASlowClaimOfAPeerDelaysNoOtherTalkgroupOfIt(t *testing.T) {
	t.Parallel()
	const slow, fast = uint(75001), uint(75002)
	h, arrivals := orderHub(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
		return &claimSlowFor{KV: k, streams: []uint{slow}}
	})

	sent := make([]models.Packet, 0, 4)
	for i := range 2 {
		for _, c := range []struct{ tg, stream uint }{{1, slow}, {2, fast}} {
			p := makeVoicePacket(c.tg, c.stream, true, false)
			p.Repeater, p.Seq = orderPeerID, uint(i)
			sent = append(sent, p)
		}
	}
	start := time.Now()
	for _, p := range sent {
		h.RoutePacket(context.Background(), p, "openbridge")
	}

	got := map[uint][]uint{}
	at := map[uint]time.Duration{}
	deadline := time.After(30 * time.Second)
	for len(got[slow]) < 2 || len(got[fast]) < 2 {
		select {
		case a := <-arrivals:
			if a.server != "mmdvm" || a.rp.RepeaterID != 100002 {
				continue
			}
			s := a.rp.Packet.StreamID
			got[s] = append(got[s], a.rp.Packet.Seq)
			at[s] = a.at.Sub(start)
		case <-deadline:
			t.Fatalf("deliveries %v", got)
		}
	}
	t.Logf("TG 1 done at %v, TG 2 done at %v", at[slow], at[fast])
	assert.Equal(t, []uint{0, 1}, got[slow])
	assert.Equal(t, []uint{0, 1}, got[fast])
	assert.Less(t, at[fast], 50*time.Millisecond, "the call to TG 2 waited for the claim of the call to TG 1")
}

// slowTopic delivers the messages of the subscriptions to topic, and of
// every feed, after delay, as a slow connection of one Redis subscription
// does.
type slowTopic struct {
	pubsub.PubSub
	topic string
	delay time.Duration
}

type delayedSubscription struct {
	pubsub.Subscription
	ch chan []byte
}

func (d *delayedSubscription) Channel() <-chan []byte { return d.ch }

func (s *slowTopic) Subscribe(topic string) pubsub.Subscription {
	sub := s.PubSub.Subscribe(topic)
	if topic != s.topic {
		return sub
	}
	d := &delayedSubscription{Subscription: sub, ch: make(chan []byte, 100)}
	go func() {
		defer close(d.ch)
		for msg := range sub.Channel() {
			time.Sleep(s.delay)
			d.ch <- msg
		}
	}()
	return d
}

type delayedFeed struct {
	pubsub.Feed
	ch chan pubsub.Message
}

func (d *delayedFeed) Channel() <-chan pubsub.Message { return d.ch }

func (s *slowTopic) Feed() pubsub.Feed {
	f := s.PubSub.Feed()
	d := &delayedFeed{Feed: f, ch: make(chan pubsub.Message, 100)}
	go func() {
		defer close(d.ch)
		for msg := range f.Channel() {
			time.Sleep(s.delay)
			d.ch <- msg
		}
	}()
	return d
}

// TestARepeaterGetsTheCallsOfASourceInOrderAcrossTopics sends a call to
// TG 1, then at once a call to TG 2 and a private call to the target
// repeater, while the messages of TG 1 reach the subscriptions to it, and
// those of a feed reach it, 100 ms late. The repeater, which listens on
// both talkgroups, must get the three calls in the order they came.
func TestARepeaterGetsTheCallsOfASourceInOrderAcrossTopics(t *testing.T) {
	t.Parallel()
	h, arrivals := orderHub(t, func(ps pubsub.PubSub) pubsub.PubSub {
		return &slowTopic{PubSub: ps, topic: "hub:packets:talkgroup:1", delay: 100 * time.Millisecond}
	}, func(k kv.KV) kv.KV { return k })

	calls := []struct {
		stream, dst uint
		group       bool
	}{{76001, 1, true}, {76002, 2, true}, {76003, 100002, false}}
	streams := map[uint]bool{}
	sent := make([]models.Packet, 0, 2*len(calls))
	for _, c := range calls {
		streams[c.stream] = true
		p := makeVoicePacket(c.dst, c.stream, c.group, false)
		term := makeVoiceTermPacket(c.dst, c.stream, c.group, false)
		term.Seq = 1
		sent = append(sent, p, term)
	}
	for _, p := range sent {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}

	got := receiveOrder(t, arrivals, streams, len(sent), "mmdvm:100002")
	assert.Equal(t, orderOf(sent), got["mmdvm:100002"])
}

// TestASlowClaimKeepsTheTimesOfTheCall sends a call of 180 ms whose claim
// the KV store answers in 500 ms, so the hub holds all of its bursts for
// StreamClaimWait. The call tracker must time the call from when the
// bursts came, not from when the hub let them in: it must keep the call,
// and not drop it as a key-up of no length.
func TestASlowClaimKeepsTheTimesOfTheCall(t *testing.T) {
	t.Parallel()
	const stream = uint(75001)
	h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
		return &claimSlowFor{KV: k, streams: []uint{stream}}
	})
	seedUser(t, database, testSrcID, "CALLER")
	seedRepeater(t, database, testRepeaterID, testSrcID)
	seedTalkgroup(t, database, 1, "TG1")
	h.ActivateRepeater(context.Background(), testRepeaterID)

	sent := make([]models.Packet, 0, 4)
	for i := range 3 {
		p := makeVoicePacket(1, stream, true, false)
		p.Seq = uint(i)
		sent = append(sent, p)
	}
	term := makeVoiceTermPacket(1, stream, true, false)
	term.Seq = 3
	sent = append(sent, term)
	for i, p := range sent {
		if i > 0 {
			time.Sleep(burstPeriod)
		}
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}

	var calls []models.Call
	require.Eventually(t, func() bool {
		calls = nil
		return database.Unscoped().Where("stream_id = ?", stream).Find(&calls).Error == nil && len(calls) == 1 && !calls[0].Active
	}, 10*time.Second, 10*time.Millisecond, "the call tracker did not keep and end the call")
	assert.GreaterOrEqual(t, calls[0].Duration, time.Duration(len(sent)-1)*burstPeriod, "the call must last from its first burst to its terminator")
}
