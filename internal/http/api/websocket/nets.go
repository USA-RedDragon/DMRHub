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
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
)

// NetsWebsocket streams net start/stop events and optionally check-in
// events to connected clients.
type NetsWebsocket struct {
	websocket.Websocket
	pubsub pubsub.PubSub
}

// CreateNetsWebsocket creates a new NetsWebsocket.
func CreateNetsWebsocket(ps pubsub.PubSub) *NetsWebsocket {
	return &NetsWebsocket{
		pubsub: ps,
	}
}

func (n *NetsWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (n *NetsWebsocket) OnConnect(ctx context.Context, r *http.Request, w websocket.Writer, _ sessions.Session) func() {
	newCtx, cancel := context.WithCancel(ctx)

	eventSubscription := n.pubsub.Subscribe("net:events")
	go forward(newCtx, eventSubscription, w)

	var checkInSubscription pubsub.Subscription
	if netIDStr := r.URL.Query().Get("net_id"); netIDStr != "" {
		if netID, err := strconv.ParseUint(netIDStr, 10, 32); err == nil {
			checkInSubscription = n.pubsub.Subscribe(fmt.Sprintf("net:checkins:%d", netID))
			go forward(newCtx, checkInSubscription, w)
		}
	}

	return func() {
		closeSubscription(eventSubscription, "nets event")
		if checkInSubscription != nil {
			closeSubscription(checkInSubscription, "nets checkin")
		}
		cancel()
	}
}
