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
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/coder/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	bufferSize = 1024
	readLimit  = 4096
)

//nolint:gochecknoglobals
var (
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
)

var (
	errClientClosed = errors.New("client closed the connection")
	errStalled      = errors.New("client does not read fast enough")
	errPingTimeout  = errors.New("client did not answer a ping")
	errWriteFailed  = errors.New("write to the client failed")
	errInvalidUTF8  = errors.New("client sent a text frame that is not UTF-8")
)

type Websocket interface {
	OnMessage(ctx context.Context, r *http.Request, w Writer, session sessions.Session, msg []byte, t int)
	// OnConnect starts the work of one connection. It keeps the state of
	// the connection in the returned function, which the handler calls
	// once when the connection closes.
	OnConnect(ctx context.Context, r *http.Request, w Writer, session sessions.Session) (onDisconnect func())
}

type WSHandler struct {
	config  *config.Config
	handler Websocket
}

func CreateHandler(config *config.Config, ws Websocket) func(*gin.Context) {
	handler := &WSHandler{config: config, handler: ws}

	return func(c *gin.Context) {
		if !handler.originAllowed(c.Request) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		session := sessions.Default(c)
		conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			slog.Error("Failed to accept websocket", "error", err)
			return
		}
		conn.SetReadLimit(readLimit)
		handler.serve(session, c.Request, conn)
	}
}

func (h *WSHandler) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	if !h.config.HTTP.CORS.Enabled {
		return true
	}
	for _, host := range h.config.HTTP.CORS.Hosts {
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
}

func (h *WSHandler) serve(s sessions.Session, r *http.Request, conn *websocket.Conn) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	writer := &wsWriter{out: make(chan Message, bufferSize), done: ctx.Done(), cancel: cancel}

	onDisconnect := h.handler.OnConnect(ctx, r, writer, s)
	if onDisconnect != nil {
		defer onDisconnect()
	}

	go h.read(ctx, cancel, s, r, conn, writer)
	go keepalive(ctx, cancel, conn)
	writer.drain(ctx, conn)

	closeConn(conn, context.Cause(ctx))
}

func (h *WSHandler) read(ctx context.Context, cancel context.CancelCauseFunc, s sessions.Session, r *http.Request, conn *websocket.Conn, w *wsWriter) {
	for {
		typ, msg, err := conn.Read(ctx)
		if err != nil {
			cancel(errClientClosed)
			return
		}
		if typ == websocket.MessageText && !utf8.Valid(msg) {
			cancel(errInvalidUTF8)
			return
		}
		if typ == websocket.MessageText && strings.EqualFold(string(msg), "ping") {
			w.WriteMessage(Message{Type: int(websocket.MessageText), Data: []byte("PONG")})
			continue
		}
		h.handler.OnMessage(ctx, r, w, s, msg, int(typ))
	}
}

func keepalive(ctx context.Context, cancel context.CancelCauseFunc, conn *websocket.Conn) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		pingCtx, pingCancel := context.WithTimeout(context.WithoutCancel(ctx), pingTimeout)
		err := conn.Ping(pingCtx)
		pingCancel()
		if err != nil {
			cancel(errPingTimeout)
			return
		}
	}
}

func closeConn(conn *websocket.Conn, cause error) {
	var err error
	switch {
	case errors.Is(cause, errInvalidUTF8):
		err = conn.Close(websocket.StatusInvalidFramePayloadData, "invalid UTF-8")
	case errors.Is(cause, errClientClosed):
		err = conn.Close(websocket.StatusNormalClosure, "")
	default:
		slog.Debug("Dropping websocket client", "reason", cause)
		err = conn.CloseNow()
	}
	if err != nil && !errors.Is(err, net.ErrClosed) {
		slog.Debug("Websocket close", "error", err)
	}
}
