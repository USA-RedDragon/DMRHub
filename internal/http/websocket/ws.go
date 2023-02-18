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
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type WSHandler struct {
	wsUpgrader websocket.Upgrader
	redis      *redis.Client
}

const bufferSize = 1024

func CreateHandler(db *gorm.DB, redis *redis.Client) *WSHandler {
	return &WSHandler{
		redis: redis,
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
	}
}

func (h *WSHandler) repeaterHandler(ctx context.Context, _ sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			klog.Errorf("Failed to close websocket: %v", err)
		}
	}()

	readFailed := make(chan string)
	go func() {
		for {
			t, msg, err := conn.ReadMessage()
			if err != nil {
				readFailed <- "read failed"
				break
			}
			if string(msg) == "PING" {
				msg = []byte("PONG")
				err = conn.WriteMessage(t, msg)
				if err != nil {
					readFailed <- "write failed"
					break
				}
				continue
			}

			err = conn.WriteMessage(t, msg)
			if err != nil {
				readFailed <- "write failed"
				break
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-readFailed:
	}
}

func (h *WSHandler) peerHandler(ctx context.Context, _ sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			klog.Errorf("Failed to close websocket: %v", err)
		}
	}()

	readFailed := make(chan string)
	go func() {
		for {
			t, msg, err := conn.ReadMessage()
			if err != nil {
				readFailed <- "read failed"
				break
			}
			if string(msg) == "PING" {
				msg = []byte("PONG")
				err = conn.WriteMessage(t, msg)
				if err != nil {
					readFailed <- "write failed"
					break
				}
				continue
			}

			err = conn.WriteMessage(t, msg)
			if err != nil {
				readFailed <- "write failed"
				break
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-readFailed:
	}
}

func (h *WSHandler) callHandler(ctx context.Context, session sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			klog.Errorf("Failed to close websocket: %v", err)
		}
	}()

	userIDIface := session.Get("user_id")
	var pubsub *redis.PubSub
	if userIDIface == nil {
		// User ID not found, subscribe to TG calls
		pubsub = h.redis.Subscribe(ctx, "calls")
		defer func() {
			err := pubsub.Unsubscribe(ctx, "calls")
			if err != nil {
				klog.Errorf("Failed to unsubscribe from calls: %v", err)
			}
		}()
	} else {
		userID, ok := userIDIface.(uint)
		if !ok {
			klog.Errorf("Failed to convert user ID to uint")
			return
		}
		pubsub = h.redis.Subscribe(ctx, fmt.Sprintf("calls:%d", userID))
		defer func() {
			err := pubsub.Unsubscribe(ctx, fmt.Sprintf("calls:%d", userID))
			if err != nil {
				klog.Errorf("Failed to unsubscribe from calls: %v", err)
			}
		}()
	}
	defer func() {
		err := pubsub.Close()
		if err != nil {
			klog.Errorf("Failed to close pubsub: %v", err)
		}
	}()

	readFailed := make(chan string)
	go func() {
		for {
			t, msg, err := conn.ReadMessage()
			if err != nil {
				readFailed <- "read failed"
				break
			}
			if string(msg) == "PING" {
				msg = []byte("PONG")
				err = conn.WriteMessage(t, msg)
				if err != nil {
					readFailed <- "write failed"
					break
				}
				continue
			}
		}
	}()

	go func() {
		for msg := range pubsub.Channel() {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				klog.Errorf("Failed to write message to websocket: %v", err)
				readFailed <- "write failed"
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-readFailed:
	}
}

func (h *WSHandler) ApplyRoutes(r *gin.Engine, ratelimit gin.HandlerFunc, userSuspension gin.HandlerFunc) {
	r.GET("/ws/repeaters", middleware.RequireLogin(), ratelimit, userSuspension, func(c *gin.Context) {
		session := sessions.Default(c)
		h.repeaterHandler(c.Request.Context(), session, c.Writer, c.Request)
	})

	r.GET("/ws/peers", middleware.RequireLogin(), ratelimit, userSuspension, func(c *gin.Context) {
		session := sessions.Default(c)
		h.peerHandler(c.Request.Context(), session, c.Writer, c.Request)
	})

	r.GET("/ws/calls", ratelimit, func(c *gin.Context) {
		session := sessions.Default(c)
		h.callHandler(c.Request.Context(), session, c.Writer, c.Request)
	})
}
