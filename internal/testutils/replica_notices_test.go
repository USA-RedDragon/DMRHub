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

package testutils_test

import (
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// heldNotices holds the messages of topic from every subscriber until
// release is closed, and sends "publish" on events for each publish of
// topic.
type heldNotices struct {
	pubsub.PubSub
	topic   string
	events  chan string
	release chan struct{}
}

type heldSubscription struct {
	pubsub.Subscription
	ch chan []byte
}

func (s heldSubscription) Channel() <-chan []byte { return s.ch }

func (p *heldNotices) Publish(topic string, message []byte) error {
	err := p.PubSub.Publish(topic, message)
	if topic == p.topic {
		p.events <- "publish"
	}
	return err //nolint:wrapcheck
}

func (p *heldNotices) Subscribe(topic string) pubsub.Subscription {
	inner := p.PubSub.Subscribe(topic)
	if topic != p.topic {
		return inner
	}
	out := make(chan []byte, 100)
	go func() {
		defer close(out)
		<-p.release
		for m := range inner.Channel() {
			out <- m
		}
	}()
	return heldSubscription{Subscription: inner, ch: out}
}

// TestSeedHelpersWaitForTheReplicas holds the change notices of one kind
// from a second replica while a seed helper of that kind runs. The helper
// must not return before the replica took its notice: it must wait, and
// once the notices flow, return with the change taken.
func TestSeedHelpersWaitForTheReplicas(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		topic string
		on    func(*hub.Hub, func(uint)) func()
		id    uint
		seed  func(t *testing.T, s *testutils.IntegrationStack)
	}{
		{"user", "hub:users:changed", (*hub.Hub).OnUserChange, 1000002, func(t *testing.T, s *testutils.IntegrationStack) {
			s.SeedUser(t, 1000002, "N1CALL")
		}},
		{"talkgroup", "hub:talkgroups:changed", (*hub.Hub).OnTalkgroupChange, 7, func(t *testing.T, s *testutils.IntegrationStack) {
			s.SeedTalkgroup(t, 7, "TG7")
		}},
		{"repeater", "hub:repeaters:changed", (*hub.Hub).OnRepeaterChange, 100001, func(t *testing.T, s *testutils.IntegrationStack) {
			s.SeedMMDVMRepeater(t, 100001, 1000001, "password")
		}},
		{"peer", "hub:peers:changed", (*hub.Hub).OnPeerChange, 500001, func(t *testing.T, s *testutils.IntegrationStack) {
			s.SeedPeer(t, 500001, 1000001, "peer-password", "127.0.0.1", 0, true, false)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			held := &heldNotices{topic: c.topic, events: make(chan string, 16), release: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(held.release) }) }
			t.Cleanup(release)
			be := testutils.SQLiteMemoryBackend()
			be.WrapPubSub = func(p pubsub.PubSub) pubsub.PubSub {
				held.PubSub = p
				return held
			}
			stack := testutils.SetupIntegrationStack(t, be)
			stack.SeedUser(t, 1000001, "N0CALL")
			second := stack.SpawnSecondReplica(t)
			stack.StartServers(t)
			taken := make(chan uint, 16)
			t.Cleanup(c.on(second.Hub, func(id uint) {
				if id == c.id {
					taken <- id
				}
			}))

			for len(held.events) > 0 {
				<-held.events
			}
			go func() {
				c.seed(t, stack)
				held.events <- "done"
			}()
			publishes := 0
			got := testutils.Collect(held.events, testutils.DeliveryWait, func(got []string) bool {
				last := got[len(got)-1]
				if last == "publish" {
					publishes++
				}
				return last == "done" || publishes == 2
			})
			require.NotEmpty(t, got)
			require.NotEqual(t, "done", got[len(got)-1], "the seed helper returned while the replica held its notice")
			release()
			require.Contains(t, testutils.Collect(held.events, testutils.DeliveryWait, func(got []string) bool { return got[len(got)-1] == "done" }), "done", "the seed helper did not return once the notices flowed")
			select {
			case <-taken:
			default:
				t.Fatal("the replica did not take the change before the helper returned")
			}
		})
	}
}
