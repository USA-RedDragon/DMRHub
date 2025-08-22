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

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makeInMemoryPubSub(config *config.Config) (PubSub, error) {
	return inMemoryPubSub{}, nil
}

type inMemoryPubSub struct {
}

func (ps inMemoryPubSub) Publish(topic string, message []byte) error {
	return nil
}

func (ps inMemoryPubSub) Subscribe(topic string) Subscription {
	return inMemorySubscription{
		ch: make(chan []byte),
	}
}

func (ps inMemoryPubSub) Close() error {
	return nil
}

type inMemorySubscription struct {
	ch chan []byte
}

func (s inMemorySubscription) Unsubscribe() error {
	return nil
}

func (s inMemorySubscription) Close() error {
	close(s.ch)
	return nil
}

func (s inMemorySubscription) Channel() <-chan []byte {
	return s.ch
}
