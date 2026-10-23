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
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	coderWS "github.com/coder/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func originServer(t *testing.T, cors bool, hosts ...string) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("sessions", cookie.NewStore([]byte("test-secret"))))
	cfg := &config.Config{}
	cfg.HTTP.CORS.Enabled = cors
	cfg.HTTP.CORS.Hosts = hosts
	router.GET("/ws", websocket.CreateHandler(cfg, newNoopWebsocket()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func dialFrom(t *testing.T, server *httptest.Server, origin string) int {
	t.Helper()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := coderWS.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", &coderWS.DialOptions{HTTPHeader: header})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		_ = conn.CloseNow()
		return http.StatusSwitchingProtocols
	}
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func TestWebsocketOriginWithCORSOff(t *testing.T) {
	t.Parallel()
	server := originServer(t, false)
	assert.Equal(t, http.StatusSwitchingProtocols, dialFrom(t, server, server.URL), "DMRHub's own origin")
	assert.Equal(t, http.StatusForbidden, dialFrom(t, server, "https://evil.example"), "a cross-site page")
	assert.Equal(t, http.StatusForbidden, dialFrom(t, server, "null"), "an opaque origin")
	assert.Equal(t, http.StatusForbidden, dialFrom(t, server, ""), "no origin")
}

func TestWebsocketOriginWithCORSOn(t *testing.T) {
	t.Parallel()
	server := originServer(t, true, "https://dmrhub.example", "radio.example:443")
	assert.Equal(t, http.StatusSwitchingProtocols, dialFrom(t, server, server.URL), "DMRHub's own origin")
	assert.Equal(t, http.StatusSwitchingProtocols, dialFrom(t, server, "https://dmrhub.example"))
	assert.Equal(t, http.StatusSwitchingProtocols, dialFrom(t, server, "https://radio.example"))
	assert.Equal(t, http.StatusForbidden, dialFrom(t, server, "http://dmrhub.example"), "another scheme")
	assert.Equal(t, http.StatusForbidden, dialFrom(t, server, "https://evil-dmrhub.example"), "a host that only contains an allowed one")
	assert.Equal(t, http.StatusForbidden, dialFrom(t, server, "https://dmrhub.example.evil"), "a host that only starts with an allowed one")
}
