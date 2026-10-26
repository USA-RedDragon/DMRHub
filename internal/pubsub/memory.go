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
		feeds:  make(map[string]map[*inMemoryFeed]struct{}),
		all:    make(map[*inMemoryFeed]struct{}),
	}, nil
}

type inMemoryPubSub struct {
	mu     sync.RWMutex
	topics map[string][]*inMemorySubscription
	feeds  map[string]map[*inMemoryFeed]struct{}
	all    map[*inMemoryFeed]struct{}
	// publishing makes one Publish at a time, so that every subscriber of
	// a topic gets its messages in the same order, as with Redis.
	publishing sync.Mutex
}

func (ps *inMemoryPubSub) Publish(topic string, message []byte) error {
	ps.publishing.Lock()
	defer ps.publishing.Unlock()
	ps.mu.RLock()
	subs := ps.topics[topic]
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
	for f := range ps.feeds[topic] {
		select {
		case f.ch <- Message{Topic: topic, Payload: append([]byte(nil), message...)}:
		default:
			slog.Warn("Dropping message for slow in-memory feed", "topic", topic)
		}
	}
	ps.mu.RUnlock()
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
			sub.closeCh()
		}
		delete(ps.topics, topic)
	}
	for f := range ps.all {
		f.closeCh()
	}
	clear(ps.feeds)
	clear(ps.all)
	return nil
}

// feedQueue is how many messages an in-memory Feed holds for its reader.
const feedQueue = 500

func (ps *inMemoryPubSub) Feed() Feed {
	f := &inMemoryFeed{ps: ps, ch: make(chan Message, feedQueue), topics: map[string]struct{}{}}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.all[f] = struct{}{}
	return f
}

type inMemoryFeed struct {
	ps        *inMemoryPubSub
	ch        chan Message
	topics    map[string]struct{}
	closed    bool
	closeOnce sync.Once
}

func (f *inMemoryFeed) Add(topic string) {
	f.ps.mu.Lock()
	defer f.ps.mu.Unlock()
	if f.closed {
		return
	}
	if f.ps.feeds[topic] == nil {
		f.ps.feeds[topic] = map[*inMemoryFeed]struct{}{}
	}
	f.ps.feeds[topic][f] = struct{}{}
	f.topics[topic] = struct{}{}
}

func (f *inMemoryFeed) Remove(topic string) {
	f.ps.mu.Lock()
	defer f.ps.mu.Unlock()
	f.removeLocked(topic)
}

func (f *inMemoryFeed) removeLocked(topic string) {
	delete(f.topics, topic)
	if feeds, ok := f.ps.feeds[topic]; ok {
		delete(feeds, f)
		if len(feeds) == 0 {
			delete(f.ps.feeds, topic)
		}
	}
}

func (f *inMemoryFeed) Channel() <-chan Message {
	return f.ch
}

func (f *inMemoryFeed) closeCh() {
	f.closeOnce.Do(func() {
		f.closed = true
		close(f.ch)
	})
}

func (f *inMemoryFeed) Close() error {
	f.ps.mu.Lock()
	defer f.ps.mu.Unlock()
	for topic := range f.topics {
		f.removeLocked(topic)
	}
	delete(f.ps.all, f)
	f.closeCh()
	return nil
}

type inMemorySubscription struct {
	ch        chan []byte
	ps        *inMemoryPubSub
	topic     string
	closeOnce sync.Once
}

// closeCh closes the channel exactly once, safe against double-close.
func (s *inMemorySubscription) closeCh() {
	s.closeOnce.Do(func() {
		close(s.ch)
	})
}

func (s *inMemorySubscription) Close() error {
	s.ps.removeSub(s.topic, s)
	s.closeCh()
	return nil
}

func (s *inMemorySubscription) Channel() <-chan []byte {
	return s.ch
}
