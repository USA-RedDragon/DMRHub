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
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	gorillaWebsocket "github.com/gorilla/websocket"
)

// NetsWebsocket streams net start/stop events and optionally check-in
// events to connected clients.
type NetsWebsocket struct {
	websocket.Websocket
	pubsub              pubsub.PubSub
	eventSubscription   pubsub.Subscription
	checkInSubscription pubsub.Subscription
	cancel              context.CancelFunc
}

// CreateNetsWebsocket creates a new NetsWebsocket.
func CreateNetsWebsocket(ps pubsub.PubSub) *NetsWebsocket {
	return &NetsWebsocket{
		pubsub: ps,
	}
}

func (n *NetsWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (n *NetsWebsocket) OnConnect(ctx context.Context, r *http.Request, w websocket.Writer, _ sessions.Session) {
	newCtx, cancel := context.WithCancel(ctx)
	n.cancel = cancel

	// Always subscribe to net lifecycle events.
	n.eventSubscription = n.pubsub.Subscribe("net:events")

	go func() {
		channel := n.eventSubscription.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case <-newCtx.Done():
				return
			case msg, ok := <-channel:
				if !ok {
					return
				}
				w.WriteMessage(websocket.Message{
					Type: gorillaWebsocket.TextMessage,
					Data: msg,
				})
			}
		}
	}()

	// If a net_id query param is provided, also subscribe to that net's
	// check-in topic so the client gets live check-in events.
	netIDStr := r.URL.Query().Get("net_id")
	if netIDStr != "" {
		netID, err := strconv.ParseUint(netIDStr, 10, 32)
		if err == nil {
			topic := fmt.Sprintf("net:checkins:%d", netID)
			n.checkInSubscription = n.pubsub.Subscribe(topic)

			go func() {
				ch := n.checkInSubscription.Channel()
				for {
					select {
					case <-ctx.Done():
						return
					case <-newCtx.Done():
						return
					case msg, ok := <-ch:
						if !ok {
							return
						}
						w.WriteMessage(websocket.Message{
							Type: gorillaWebsocket.TextMessage,
							Data: msg,
						})
					}
				}
			}()
		}
	}
}

func (n *NetsWebsocket) OnDisconnect(_ context.Context, _ *http.Request, _ sessions.Session) {
	if n.eventSubscription != nil {
		if err := n.eventSubscription.Close(); err != nil {
			slog.Error("Failed to close nets event pubsub subscription", "error", err)
		}
	}
	if n.checkInSubscription != nil {
		if err := n.checkInSubscription.Close(); err != nil {
			slog.Error("Failed to close nets checkin pubsub subscription", "error", err)
		}
	}
	if n.cancel != nil {
		n.cancel()
	}
}
