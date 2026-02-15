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
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	gorillaWS "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageStruct(t *testing.T) {
	t.Parallel()
	msg := websocket.Message{
		Type: 1,
		Data: []byte("hello"),
	}
	assert.Equal(t, 1, msg.Type)
	assert.Equal(t, []byte("hello"), msg.Data)
}

func TestMessageEmptyData(t *testing.T) {
	t.Parallel()
	msg := websocket.Message{
		Type: 2,
		Data: nil,
	}
	assert.Equal(t, 2, msg.Type)
	assert.Nil(t, msg.Data)
}

func TestMessageBinaryData(t *testing.T) {
	t.Parallel()
	data := []byte{0x00, 0x01, 0x02, 0xFF}
	msg := websocket.Message{
		Type: 2,
		Data: data,
	}
	assert.Equal(t, 2, msg.Type)
	assert.Equal(t, data, msg.Data)
	assert.Len(t, msg.Data, 4)
}

// noopWebsocket is a minimal Websocket implementation for testing.
type noopWebsocket struct {
	mu        sync.Mutex
	connectN  int
	disconnN  int
	connectCh chan struct{}
	disconnCh chan struct{}
}

func newNoopWebsocket() *noopWebsocket {
	return &noopWebsocket{
		connectCh: make(chan struct{}, 10),
		disconnCh: make(chan struct{}, 10),
	}
}

func (n *noopWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (n *noopWebsocket) OnConnect(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session) {
	n.mu.Lock()
	n.connectN++
	n.mu.Unlock()
	n.connectCh <- struct{}{}
}

func (n *noopWebsocket) OnDisconnect(_ context.Context, _ *http.Request, _ sessions.Session) {
	n.mu.Lock()
	n.disconnN++
	n.mu.Unlock()
	n.disconnCh <- struct{}{}
}

func (n *noopWebsocket) Connects() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.connectN
}

func (n *noopWebsocket) Disconnects() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.disconnN
}

func setupTestServer(t *testing.T, ws *noopWebsocket) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	router.Use(sessions.Sessions("sessions", store))

	cfg := &config.Config{}
	cfg.HTTP.CORS.Enabled = false

	router.GET("/ws", websocket.CreateHandler(cfg, ws))
	return httptest.NewServer(router)
}

func dialWS(t *testing.T, serverURL string) *gorillaWS.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws"
	dialer := gorillaWS.Dialer{}
	header := http.Header{}
	header.Set("Origin", serverURL)
	conn, resp, err := dialer.Dial(wsURL, header)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return conn
}

// TestConcurrentWebSocketUpgrades verifies that multiple concurrent WebSocket
// connections are handled independently without racing on shared state.
// Before the fix, a single WSHandler.conn field was shared and mutated per
// request, causing a data race under concurrent upgrades.
func TestConcurrentWebSocketUpgrades(t *testing.T) {
	t.Parallel()

	ws := newNoopWebsocket()
	server := setupTestServer(t, ws)
	defer server.Close()

	const numClients = 5
	var wg sync.WaitGroup
	conns := make([]*gorillaWS.Conn, numClients)

	// Open all connections concurrently
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conns[idx] = dialWS(t, server.URL)
		}(i)
	}
	wg.Wait()

	// Wait for all OnConnect callbacks
	for i := 0; i < numClients; i++ {
		select {
		case <-ws.connectCh:
		case <-time.After(5 * time.Second):
			t.Fatal("Timed out waiting for OnConnect")
		}
	}

	assert.Equal(t, numClients, ws.Connects())

	// Close all connections
	for _, conn := range conns {
		_ = conn.Close()
	}

	// Wait for all OnDisconnect callbacks
	for i := 0; i < numClients; i++ {
		select {
		case <-ws.disconnCh:
		case <-time.After(5 * time.Second):
			t.Fatal("Timed out waiting for OnDisconnect")
		}
	}

	assert.Equal(t, numClients, ws.Disconnects())
}

// TestReaderGoroutineDoesNotLeak verifies that the reader goroutine exits
// cleanly when the connection is closed. Before the fix, the reader
// goroutine would block forever on an unbuffered error channel if the
// main select loop exited via context cancellation first.
func TestReaderGoroutineDoesNotLeak(t *testing.T) {
	t.Parallel()

	ws := newNoopWebsocket()
	server := setupTestServer(t, ws)
	defer server.Close()

	conn := dialWS(t, server.URL)

	select {
	case <-ws.connectCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for OnConnect")
	}

	// Close the connection from the client side. This causes the server's
	// ReadMessage to return an error. With the fix, the reader goroutine
	// sends on the buffered error channel and exits without blocking.
	_ = conn.Close()

	select {
	case <-ws.disconnCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Reader goroutine leaked: OnDisconnect was never called")
	}

	assert.Equal(t, 1, ws.Disconnects())
}
