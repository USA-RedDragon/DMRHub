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
	"net/http"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"k8s.io/klog/v2"
)

const bufferSize = 1024

type Websocket interface {
	OnMessage(ctx context.Context, r *http.Request, w WebsocketWriter, session sessions.Session, msg []byte, t int)
	OnConnect(ctx context.Context, r *http.Request, w WebsocketWriter, session sessions.Session)
	OnDisconnect(ctx context.Context, r *http.Request, session sessions.Session)
}

type WSHandler struct {
	wsUpgrader websocket.Upgrader
	handler    Websocket
	conn       *websocket.Conn
}

func CreateHandler(ws Websocket) func(*gin.Context) {
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
				for _, host := range config.GetConfig().CORSHosts {
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
		err := handler.Upgrade(c.Request.Context(), c.Writer, c.Request)
		if err != nil {
			klog.Errorf("Failed to set websocket upgrade: %v", err)
			return
		}
		defer func() {
			handler.handler.OnDisconnect(c, c.Request, session)
			err := handler.Close()
			if err != nil {
				klog.Errorf("Failed to close websocket: %v", err)
			}
		}()
		handler.Handle(c.Request.Context(), session, c.Writer, c.Request)
	}
}

func (h *WSHandler) Upgrade(c context.Context, w gin.ResponseWriter, r *http.Request) error {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	h.conn = conn
	return nil
}

func (h *WSHandler) Close() error {
	err := h.conn.Close()
	if err != nil {
		return err
	}
	return nil
}

func (h *WSHandler) Handle(c context.Context, s sessions.Session, w gin.ResponseWriter, r *http.Request) {
	writer := wsWriter{
		writer: make(chan WebsocketMessage, bufferSize),
		error:  make(chan string),
	}
	h.handler.OnConnect(c, r, writer, s)

	go func() {
		for {
			t, msg, err := h.conn.ReadMessage()
			if err != nil {
				writer.Error("read failed")
				break
			}
			if t == websocket.PingMessage {
				writer.WriteMessage(WebsocketMessage{
					Type: websocket.PongMessage,
				})
			} else if strings.EqualFold(string(msg), "ping") {
				writer.WriteMessage(WebsocketMessage{
					Type: websocket.TextMessage,
					Data: []byte("PONG"),
				})
			} else {
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
			err := h.conn.WriteMessage(msg.Type, msg.Data)
			if err != nil {
				return
			}
		}
	}
}
