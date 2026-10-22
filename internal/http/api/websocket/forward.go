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

	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	gorillaWebsocket "github.com/gorilla/websocket"
)

func forward(ctx context.Context, subscription pubsub.Subscription, w websocket.Writer) {
	channel := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
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
}

func closeSubscription(subscription pubsub.Subscription, name string) {
	if err := subscription.Close(); err != nil {
		slog.Error("Failed to close pubsub subscription", "websocket", name, "error", err)
	}
}
