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
	"context"
	"errors"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

// ErrNotSent is in the error of a Publish whose message did not reach the
// broker, such as when no connection to it could be made. A publish that
// fails with another error may still deliver its message, so only a
// message that failed with ErrNotSent may be published again without the
// risk of a second delivery.
var ErrNotSent = errors.New("the message did not reach the broker")

type PubSub interface {
	Publish(topic string, message []byte) error
	Subscribe(topic string) Subscription
	// Feed returns a new Feed with no topics.
	Feed() Feed
	Close() error
}

type Subscription interface {
	Close() error
	Channel() <-chan []byte
}

// Feed is one subscription to a set of topics that can change. Its
// messages come on one channel, in the order in which their topics got
// them.
type Feed interface {
	// Add subscribes the Feed to topic and returns once every message
	// published after it returns comes, as Subscribe does.
	Add(topic string)
	// Remove ends the subscription to topic. Messages of topic that were
	// on their way can still come.
	Remove(topic string)
	Channel() <-chan Message
	Close() error
}

// Message is a message of a Feed and its topic.
type Message struct {
	Topic   string
	Payload []byte
}

func MakePubSub(ctx context.Context, config *config.Config) (PubSub, error) {
	if config.Redis.Enabled {
		return makePubSubFromRedis(ctx, config)
	}
	return makeInMemoryPubSub(config)
}
