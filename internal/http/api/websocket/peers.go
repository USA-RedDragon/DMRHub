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
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	"gorm.io/gorm"
)

type PeersWebsocket struct {
	websocket.Websocket
	pubsub pubsub.PubSub
	db     *gorm.DB
}

func CreatePeersWebsocket(db *gorm.DB, pubsub pubsub.PubSub) *PeersWebsocket {
	return &PeersWebsocket{
		pubsub: pubsub,
		db:     db,
	}
}

func (c *PeersWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (c *PeersWebsocket) OnConnect(ctx context.Context, _ *http.Request, w websocket.Writer, _ sessions.Session) func() {
	newCtx, cancel := context.WithCancel(ctx)
	subscription := c.pubsub.Subscribe("hub:events:peers")
	go forward(newCtx, subscription, w)
	return func() {
		closeSubscription(subscription, "peers")
		cancel()
	}
}
