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

package websocket_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	coderWS "github.com/coder/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdWebsocket holds each message in OnMessage until release closes.
type holdWebsocket struct {
	held        chan struct{}
	release     chan struct{}
	disconnects atomic.Int64
}

func (h *holdWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
	h.held <- struct{}{}
	<-h.release
}

func (h *holdWebsocket) OnConnect(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session) func() {
	return func() {
		h.disconnects.Add(1)
	}
}

// TestConnsCloseEndsEachWebsocketAndWaitsForIt closes the Conns of a router
// while the handler of its websocket still runs an OnMessage. The client
// gets status 1001, Close returns only after the OnMessage and then
// onDisconnect have run, and a websocket opened after Close is refused.
func TestConnsCloseEndsEachWebsocketAndWaitsForIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		conns := websocket.NewConns()
		router := gin.New()
		router.Use(conns.Track())
		router.Use(sessions.Sessions("sessions", cookie.NewStore([]byte("test-secret"))))
		ws := &holdWebsocket{held: make(chan struct{}, 1), release: make(chan struct{})}
		router.GET("/ws", websocket.CreateHandler(&config.Config{}, ws))
		server := startPipeServer(t, router)
		defer server.Close()

		conn := dialWS(t, server)
		defer func() { _ = conn.CloseNow() }()
		ctx, cancel := context.WithTimeout(context.Background(), wsWait)
		defer cancel()
		require.NoError(t, conn.Write(ctx, coderWS.MessageText, []byte("hold")))
		<-ws.held

		closed := make(chan error, 1)
		go func() { closed <- conns.Close(context.Background()) }()
		_, _, err := conn.Read(ctx)
		assert.Equal(t, coderWS.StatusGoingAway, coderWS.CloseStatus(err), "the close frame: %v", err)
		synctest.Wait()
		select {
		case err := <-closed:
			t.Fatalf("Close returned while an OnMessage of the websocket still ran: %v", err)
		default:
		}
		assert.Zero(t, ws.disconnects.Load(), "onDisconnect ran before the last OnMessage ended")

		close(ws.release)
		require.NoError(t, <-closed)
		assert.Equal(t, int64(1), ws.disconnects.Load())

		header := http.Header{}
		header.Set("Origin", server.URL)
		_, resp, err := coderWS.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", &coderWS.DialOptions{HTTPHeader: header, HTTPClient: server.client})
		require.Error(t, err)
		require.NotNil(t, resp)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "a websocket opened after Close")
	})
}
