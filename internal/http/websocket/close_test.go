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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	coderWS "github.com/coder/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserChangeDuringAWriteStillSendsTheCloseFrame(t *testing.T) {
	t.Parallel()
	ps, err := pubsub.MakePubSub(context.Background(), &config.Config{})
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("sessions", cookie.NewStore([]byte("test-secret"))))
	router.Use(func(c *gin.Context) { sessions.Default(c).Set("user_id", uint(7)) })
	ws := newCaptureWebsocket()
	router.GET("/ws", websocket.WatchUser(ps), websocket.CreateHandler(&config.Config{}, ws))
	server := httptest.NewServer(router)
	defer server.Close()

	conn := dialWS(t, server.URL)
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(64 << 20)
	w := <-ws.writers

	big := []byte(strings.Repeat("x", 32<<20))
	w.WriteMessage(websocket.Message{Type: int(coderWS.MessageText), Data: big})
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, websocket.PublishUserChanged(ps, 7))
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, got, err := conn.Read(ctx)
	require.NoError(t, err, "the message being written when the user changed arrives whole")
	require.Len(t, got, len(big))
	_, _, err = conn.Read(ctx)
	require.Equal(t, coderWS.StatusPolicyViolation, coderWS.CloseStatus(err), "then the close frame: %v", err)
}
