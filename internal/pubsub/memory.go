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

package pubsub

import (
	"log/slog"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

func makeInMemoryPubSub(_ *config.Config) (PubSub, error) {
	return &inMemoryPubSub{
		topics: make(map[string][]*inMemorySubscription),
	}, nil
}

type inMemoryPubSub struct {
	mu     sync.RWMutex
	topics map[string][]*inMemorySubscription
}

func (ps *inMemoryPubSub) Publish(topic string, message []byte) error {
	ps.mu.RLock()
	subs := make([]*inMemorySubscription, len(ps.topics[topic]))
	copy(subs, ps.topics[topic])
	ps.mu.RUnlock()

	for _, sub := range subs {
		// Copy the message so each subscriber gets independent data
		msg := make([]byte, len(message))
		copy(msg, message)
		select {
		case sub.ch <- msg:
		default:
			slog.Warn("Dropping message for slow in-memory subscriber", "topic", topic)
		}
	}
	return nil
}

func (ps *inMemoryPubSub) Subscribe(topic string) Subscription {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	sub := &inMemorySubscription{
		ch:    make(chan []byte, 100),
		ps:    ps,
		topic: topic,
	}
	ps.topics[topic] = append(ps.topics[topic], sub)
	return sub
}

func (ps *inMemoryPubSub) removeSub(topic string, target *inMemorySubscription) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	subs := ps.topics[topic]
	for i, s := range subs {
		if s == target {
			ps.topics[topic] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

func (ps *inMemoryPubSub) Close() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for topic, subs := range ps.topics {
		for _, sub := range subs {
			close(sub.ch)
		}
		delete(ps.topics, topic)
	}
	return nil
}

type inMemorySubscription struct {
	ch    chan []byte
	ps    *inMemoryPubSub
	topic string
}

func (s *inMemorySubscription) Close() error {
	s.ps.removeSub(s.topic, s)
	return nil
}

func (s *inMemorySubscription) Channel() <-chan []byte {
	return s.ch
}
