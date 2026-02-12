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
	"fmt"
	"log/slog"
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	gorillaWebsocket "github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type CallsWebsocket struct {
	websocket.Websocket
	hub          *hub.Hub
	pubsub       pubsub.PubSub
	db           *gorm.DB
	subscription pubsub.Subscription
	cancel       context.CancelFunc
}

func CreateCallsWebsocket(dmrHub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub) *CallsWebsocket {
	return &CallsWebsocket{
		hub:    dmrHub,
		pubsub: pubsub,
		db:     db,
	}
}

func (c *CallsWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (c *CallsWebsocket) OnConnect(ctx context.Context, _ *http.Request, w websocket.Writer, session sessions.Session) {
	newCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	userIDIface := session.Get("user_id")
	if userIDIface == nil {
		// User ID not found, subscribe to public calls
		c.subscription = c.pubsub.Subscribe("calls:public")
	} else {
		userID, ok := userIDIface.(uint)
		if !ok {
			slog.Error("Failed to convert user ID to uint", "userIDIface", userIDIface)
			return
		}
		go c.hub.ListenForWebsocket(newCtx, userID)
		c.subscription = c.pubsub.Subscribe(fmt.Sprintf("calls:%d", userID))
	}

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

func (c *CallsWebsocket) OnDisconnect(_ context.Context, _ *http.Request, _ sessions.Session) {
	err := c.subscription.Close()
	if err != nil {
		slog.Error("Failed to close pubsub", "error", err)
	}
	c.cancel()
}
