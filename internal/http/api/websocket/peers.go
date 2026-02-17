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
	"log/slog"
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	gorillaWebsocket "github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type PeersWebsocket struct {
	websocket.Websocket
	pubsub       pubsub.PubSub
	db           *gorm.DB
	subscription pubsub.Subscription
	cancel       context.CancelFunc
}

func CreatePeersWebsocket(db *gorm.DB, pubsub pubsub.PubSub) *PeersWebsocket {
	return &PeersWebsocket{
		pubsub: pubsub,
		db:     db,
	}
}

func (c *PeersWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (c *PeersWebsocket) OnConnect(ctx context.Context, _ *http.Request, w websocket.Writer, _ sessions.Session) {
	newCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.subscription = c.pubsub.Subscribe("hub:events:peers")

	go func() {
		channel := c.subscription.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case <-newCtx.Done():
				return
			case msg := <-channel:
				w.WriteMessage(websocket.Message{
					Type: gorillaWebsocket.TextMessage,
					Data: msg,
				})
			}
		}
	}()
}

func (c *PeersWebsocket) OnDisconnect(_ context.Context, _ *http.Request, _ sessions.Session) {
	if c.subscription != nil {
		err := c.subscription.Close()
		if err != nil {
			slog.Error("Failed to close peers pubsub", "error", err)
		}
	}
	if c.cancel != nil {
		c.cancel()
	}
}
