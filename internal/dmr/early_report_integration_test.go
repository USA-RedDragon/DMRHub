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

package dmr_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type heldFeeds struct {
	pubsub.PubSub
	releaseTopic string
	mu           sync.Mutex
	release      chan struct{}
	came         atomic.Bool
}

func (h *heldFeeds) hold() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.release = make(chan struct{})
}

func (h *heldFeeds) let() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.release != nil {
		close(h.release)
		h.release = nil
	}
}

func (h *heldFeeds) gate() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.release
}

func (h *heldFeeds) Publish(topic string, msg []byte) error {
	err := h.PubSub.Publish(topic, msg)
	if topic == h.releaseTopic {
		h.let()
	}
	if err != nil {
		return fmt.Errorf("publishing to %s: %w", topic, err)
	}
	return nil
}

type heldFeed struct {
	pubsub.Feed
	ch   chan pubsub.Message
	stop chan struct{}
	once sync.Once
}

func (h *heldFeeds) Feed() pubsub.Feed {
	inner := h.PubSub.Feed()
	f := &heldFeed{Feed: inner, ch: make(chan pubsub.Message), stop: make(chan struct{})}
	in := make(chan pubsub.Message, 1024)
	go func() {
		defer close(in)
		for m := range inner.Channel() {
			if strings.HasPrefix(string(m.Payload), "hub:settle:") {
				h.let()
			} else if h.gate() != nil {
				h.came.Store(true)
			}
			select {
			case in <- m:
			case <-f.stop:
				return
			}
		}
	}()
	go func() {
		defer close(f.ch)
		for m := range in {
			if g := h.gate(); g != nil {
				select {
				case <-g:
				case <-f.stop:
					return
				}
			}
			select {
			case f.ch <- m:
			case <-f.stop:
				return
			}
		}
	}()
	return f
}

func (f *heldFeed) Channel() <-chan pubsub.Message { return f.ch }

func (f *heldFeed) Close() error {
	f.once.Do(func() { close(f.stop) })
	if err := f.Feed.Close(); err != nil {
		return fmt.Errorf("closing the held feed: %w", err)
	}
	return nil
}

// TestTheEarlyIPSCReportHoldsTheBurstAtTheMove sends a burst of a call to
// an IPSC peer just before the peer moves to the replica of the talker,
// and holds the burst on its way to the replica that served the peer, as
// a starved one is slow to take it. The replica of the talker subscribed
// the peer only after the burst went out, so only the early report of the
// old replica can bring it in time for its slot: the report must hold it.
func TestTheEarlyIPSCReportHoldsTheBurstAtTheMove(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const stream = 7701
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.AssignTS1StaticTG(t, 200001, 1)
		earlyTopic := "hub:handover-early:" + models.RepeaterTypeIPSC + ":200001"
		held := &heldFeeds{PubSub: stack.PubSub, releaseTopic: earlyTopic}
		shared := stack.PubSub
		stack.PubSub = held
		old, regOld := stack.SpawnRegisteredReplica(t)
		stack.PubSub = shared
		testutils.Register(t, stack.Hub, stack.KV)
		stack.StartServers(t)
		early := stack.PubSub.Subscribe(earlyTopic)
		t.Cleanup(func() { _ = early.Close() })

		front := testutils.NewUDPFront(t)
		front.Add(t, old.IPSCAddr)
		peer := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, peer.Connect(front.Addr()))
		t.Cleanup(peer.Close)
		require.NoError(t, peer.WaitReady(handshakeWait))
		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, talker.Connect(stack.MMDVMAddr))
		t.Cleanup(talker.Close)
		require.NoError(t, talker.WaitReady(handshakeWait))
		waitClaimed(t, stack, 200001)
		waitSubscribed(t, old.Hub, 200001, 1, true)
		front.Add(t, stack.IPSCAddr)
		waitFor(t, "the peer has a keepalive to send", func() bool { return peer.SendAlive() == nil })

		held.hold()
		burst := makeGroupVoicePacket(1000001, 1, stream, false)
		burst.Seq = 7
		require.NoError(t, talker.SendDMRD(burst))
		waitFor(t, "the burst reaches the feed of the old replica", held.came.Load)
		regOld.MarkDraining(t.Context())
		front.Drain(old.IPSCAddr)
		require.NoError(t, peer.SendAlive())

		var report struct {
			Lost [][]byte `json:"lost"`
		}
		var b []byte
		waitFor(t, "the early report", func() bool {
			select {
			case b = <-early.Channel():
				return true
			default:
				return false
			}
		})
		require.NoError(t, json.Unmarshal(b, &report))
		var seqs []uint
		for _, raw := range report.Lost {
			var p models.Packet
			if _, err := p.UnmarshalMsg(raw); err == nil && p.StreamID == stream {
				seqs = append(seqs, p.Seq)
			}
		}
		assert.Contains(t, seqs, burst.Seq, "the early report misses the burst that went to the old replica before the move")
	})
}
