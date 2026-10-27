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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	"gorm.io/gorm"
)

type CallsWebsocket struct {
	websocket.Websocket
	hub    *hub.Hub
	pubsub pubsub.PubSub
	db     *gorm.DB
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

func (c *CallsWebsocket) OnConnect(ctx context.Context, _ *http.Request, w websocket.Writer, session sessions.Session) func() {
	newCtx, cancel := context.WithCancel(ctx)

	var subscription pubsub.Subscription
	userIDIface := session.Get("user_id")
	if userIDIface == nil {
		subscription = c.pubsub.Subscribe("calls:public")
	} else {
		userID, ok := userIDIface.(uint)
		if !ok {
			slog.Error("Failed to convert user ID to uint", "userIDIface", userIDIface)
			return cancel
		}
		go c.hub.ListenForWebsocket(newCtx, userID)
		subscription = c.pubsub.Subscribe(fmt.Sprintf("calls:%d", userID))
	}

	go forward(newCtx, subscription, w)
	return func() {
		closeSubscription(subscription, "calls")
		cancel()
	}
}

// PublicCallsWebsocket sends the public calls, which the last heard page
// shows to everyone, whoever opens it.
type PublicCallsWebsocket struct {
	current config.Source
	pubsub  pubsub.PubSub
}

// CreatePublicCallsWebsocket returns the websocket of the public calls.
// A visitor who is not logged in gets no calls while visibility is login,
// and the calls without names and positions while it is hide-names.
func CreatePublicCallsWebsocket(current config.Source, pubsub pubsub.PubSub) *PublicCallsWebsocket {
	return &PublicCallsWebsocket{current: current, pubsub: pubsub}
}

func (c *PublicCallsWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (c *PublicCallsWebsocket) OnConnect(ctx context.Context, _ *http.Request, w websocket.Writer, session sessions.Session) func() {
	newCtx, cancel := context.WithCancel(ctx)
	subscription := c.pubsub.Subscribe("calls:public")
	if session != nil && session.Get("user_id") != nil {
		go forward(newCtx, subscription, w)
	} else {
		go forwardPublic(newCtx, subscription, w, c.current)
	}
	return func() {
		closeSubscription(subscription, "calls")
		cancel()
	}
}
