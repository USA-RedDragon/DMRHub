// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/puzpuzpuz/xsync/v4"
)

func makeInMemoryPubSub(_ *config.Config) (PubSub, error) {
	return inMemoryPubSub{
		data: xsync.NewMapOf[string, inMemorySubscription](),
	}, nil
}

type inMemoryPubSub struct {
	data *xsync.MapOf[string, inMemorySubscription]
}

func (ps inMemoryPubSub) Publish(topic string, message []byte) error {
	ps.makeChannelIfNotExists(topic)
	sub, _ := ps.data.Load(topic)
	sub.ch <- message
	return nil
}

func (ps inMemoryPubSub) makeChannelIfNotExists(topic string) {
	if _, ok := ps.data.Load(topic); !ok {
		ch := make(chan []byte, 100)
		ps.data.Store(topic, inMemorySubscription{ch: ch})
	}
}

func (ps inMemoryPubSub) Subscribe(topic string) Subscription {
	ps.makeChannelIfNotExists(topic)
	sub, _ := ps.data.Load(topic)
	return sub
}

func (ps inMemoryPubSub) Close() error {
	ps.data.Range(func(key string, value inMemorySubscription) bool {
		err := value.Close()
		if err != nil {
			slog.Error("Error closing in-memory subscription", "topic", key, "error", err)
		}
		ps.data.Delete(key)
		return true
	})
	return nil
}

type inMemorySubscription struct {
	ch chan []byte
}

func (s inMemorySubscription) Close() error {
	return nil
}

func (s inMemorySubscription) Channel() <-chan []byte {
	return s.ch
}
