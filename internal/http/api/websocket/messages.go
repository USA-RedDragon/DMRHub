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

package websocket

import (
	"context"
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
)

// MessagesWebsocket pushes to each connection the new and changed messages
// that its user sees in a box. Each connection keeps its own subscription.
type MessagesWebsocket struct {
	websocket.Websocket
	pubsub pubsub.PubSub
}

// CreateMessagesWebsocket returns the handler of /ws/messages.
func CreateMessagesWebsocket(ps pubsub.PubSub) *MessagesWebsocket {
	return &MessagesWebsocket{pubsub: ps}
}

func (c *MessagesWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (c *MessagesWebsocket) OnConnect(ctx context.Context, _ *http.Request, w websocket.Writer, session sessions.Session) func() {
	userID, ok := session.Get("user_id").(uint)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	subscription := c.pubsub.Subscribe(messages.Topic(userID))
	go forward(ctx, subscription, w)
	return func() {
		closeSubscription(subscription, "messages")
		cancel()
	}
}
