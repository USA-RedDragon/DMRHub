// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/hbrp"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/gin-contrib/sessions"
	gorillaWebsocket "github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type CallsWebsocket struct {
	websocket.Websocket
	redis        *redis.Client
	db           *gorm.DB
	subscription *redis.PubSub
	cancel       context.CancelFunc
}

func CreateCallsWebsocket(db *gorm.DB, redis *redis.Client) *CallsWebsocket {
	return &CallsWebsocket{
		redis: redis,
		db:    db,
	}
}

func (c *CallsWebsocket) OnMessage(ctx context.Context, r *http.Request, w websocket.WebsocketWriter, _ sessions.Session, msg []byte, t int) {
}

func (c *CallsWebsocket) OnConnect(ctx context.Context, r *http.Request, w websocket.WebsocketWriter, session sessions.Session) {
	newCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	userIDIface := session.Get("user_id")
	if userIDIface == nil {
		// User ID not found, subscribe to public calls
		c.subscription = c.redis.Subscribe(ctx, "calls:public")
	} else {
		userID, ok := userIDIface.(uint)
		if !ok {
			klog.Errorf("Failed to convert user ID to uint")
			return
		}
		go hbrp.GetSubscriptionManager().ListenForWebsocket(newCtx, c.db, c.redis, userID)
		c.subscription = c.redis.Subscribe(ctx, fmt.Sprintf("calls:%d", userID))
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
				w.WriteMessage(websocket.WebsocketMessage{
					Type: gorillaWebsocket.TextMessage,
					Data: []byte(msg.Payload),
				})
			}
		}
	}()
}

func (c *CallsWebsocket) OnDisconnect(ctx context.Context, r *http.Request, _ sessions.Session) {
	err := c.subscription.Close()
	if err != nil {
		klog.Errorf("Failed to close pubsub: %v", err)
	}
	c.cancel()
}
