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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	apiWebsocket "github.com/USA-RedDragon/DMRHub/internal/http/api/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	coderWS "github.com/coder/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func dial(t *testing.T, url string) *coderWS.Conn {
	t.Helper()
	header := http.Header{}
	header.Set("Origin", url)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := coderWS.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/ws", &coderWS.DialOptions{HTTPHeader: header})
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return conn
}

func TestCallsWebsocketKeepsStatePerConnection(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	ps, err := pubsub.MakePubSub(context.Background(), cfg)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("sessions", cookie.NewStore([]byte("test-secret"))))
	router.GET("/ws", websocket.CreateHandler(cfg, apiWebsocket.CreateCallsWebsocket(nil, nil, ps)))
	server := httptest.NewServer(router)
	defer server.Close()

	a := dial(t, server.URL)
	b := dial(t, server.URL)
	defer func() { _ = b.CloseNow() }()
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, a.Close(coderWS.StatusNormalClosure, ""))
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, ps.Publish("calls:public", []byte("call")))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, msg, err := b.Read(ctx)
	require.NoError(t, err, "the second connection lost its stream when the first closed")
	require.Equal(t, "call", string(msg))
}
