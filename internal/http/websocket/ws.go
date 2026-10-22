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
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const bufferSize = 1024

type Websocket interface {
	OnMessage(ctx context.Context, r *http.Request, w Writer, session sessions.Session, msg []byte, t int)
	// OnConnect starts the work of one connection. It keeps the state of
	// the connection in the returned function, which the handler calls
	// once when the connection closes.
	OnConnect(ctx context.Context, r *http.Request, w Writer, session sessions.Session) (onDisconnect func())
}

type WSHandler struct {
	wsUpgrader websocket.Upgrader
	handler    Websocket
}

func CreateHandler(config *config.Config, ws Websocket) func(*gin.Context) {
	handler := &WSHandler{
		wsUpgrader: websocket.Upgrader{
			HandshakeTimeout: 0,
			ReadBufferSize:   bufferSize,
			WriteBufferSize:  bufferSize,
			WriteBufferPool:  nil,
			Subprotocols:     []string{},
			Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
			},
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return false
				}
				// If CORS is disabled, allow all origins
				if !config.HTTP.CORS.Enabled {
					return true
				}
				for _, host := range config.HTTP.CORS.Hosts {
					if strings.HasSuffix(host, ":443") && strings.HasPrefix(origin, "https://") {
						host = strings.TrimSuffix(host, ":443")
					}
					if strings.HasSuffix(host, ":80") && strings.HasPrefix(origin, "http://") {
						host = strings.TrimSuffix(host, ":80")
					}
					if strings.Contains(origin, host) {
						return true
					}
				}
				return false
			},
			EnableCompression: true,
		},
		handler: ws,
	}

	return func(c *gin.Context) {
		session := sessions.Default(c)
		conn, err := handler.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			slog.Error("Failed to set websocket upgrade", "error", err)
			return
		}

		defer func() {
			err := conn.Close()
			if err != nil {
				slog.Error("Failed to close websocket", "error", err)
			}
		}()
		handler.handle(c.Request.Context(), session, c.Request, conn)
	}
}

func (h *WSHandler) handle(c context.Context, s sessions.Session, r *http.Request, conn *websocket.Conn) {
	writer := wsWriter{
		writer: make(chan Message, bufferSize),
		// Buffered so the reader goroutine can always send without blocking,
		// even if the main select loop has already exited (e.g. context cancelled).
		error: make(chan string, 1),
	}
	onDisconnect := h.handler.OnConnect(c, r, writer, s)
	if onDisconnect != nil {
		defer onDisconnect()
	}

	go func() {
		for {
			t, msg, err := conn.ReadMessage()
			if err != nil {
				writer.Error("read failed")
				return
			}
			switch {
			case t == websocket.PingMessage:
				writer.WriteMessage(Message{
					Type: websocket.PongMessage,
				})
			case strings.EqualFold(string(msg), "ping"):
				writer.WriteMessage(Message{
					Type: websocket.TextMessage,
					Data: []byte("PONG"),
				})
			default:
				h.handler.OnMessage(c, r, writer, s, msg, t)
			}
		}
	}()

	for {
		select {
		case <-c.Done():
			return
		case <-writer.error:
			return
		case msg := <-writer.writer:
			err := conn.WriteMessage(msg.Type, msg.Data)
			if err != nil {
				return
			}
		}
	}
}
