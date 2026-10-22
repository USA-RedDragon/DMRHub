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
	coderWS "github.com/coder/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
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

func (n *noopWebsocket) OnConnect(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session) func() {
	n.mu.Lock()
	n.connectN++
	n.mu.Unlock()
	n.connectCh <- struct{}{}
	return func() {
		n.mu.Lock()
		n.disconnN++
		n.mu.Unlock()
		n.disconnCh <- struct{}{}
	}
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

func setupTestServer(t *testing.T, ws websocket.Websocket) *httptest.Server {
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

func dialWS(t *testing.T, serverURL string) *coderWS.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws"
	header := http.Header{}
	header.Set("Origin", serverURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := coderWS.Dial(ctx, wsURL, &coderWS.DialOptions{HTTPHeader: header})
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
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
	conns := make([]*coderWS.Conn, numClients)

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
		_ = conn.CloseNow()
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
	_ = conn.CloseNow()

	select {
	case <-ws.disconnCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Reader goroutine leaked: OnDisconnect was never called")
	}

	assert.Equal(t, 1, ws.Disconnects())
}

type floodWebsocket struct {
	finished chan struct{}
}

func (f *floodWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, _ []byte, _ int) {
}

func (f *floodWebsocket) OnConnect(_ context.Context, _ *http.Request, w websocket.Writer, _ sessions.Session) func() {
	go func() {
		for range 10 * 1024 {
			w.WriteMessage(websocket.Message{Type: int(coderWS.MessageText), Data: []byte("x")})
		}
		close(f.finished)
	}()
	return nil
}

func TestWriterDoesNotBlockAfterClose(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("sessions", cookie.NewStore([]byte("test-secret"))))
	ws := &floodWebsocket{finished: make(chan struct{})}
	router.GET("/ws", websocket.CreateHandler(&config.Config{}, ws))
	server := httptest.NewServer(router)
	defer server.Close()

	conn := dialWS(t, server.URL)
	_ = conn.CloseNow()

	select {
	case <-ws.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("a writer stayed blocked after the connection closed")
	}
}

type captureWebsocket struct {
	writers  chan websocket.Writer
	messages chan []byte
	closed   chan struct{}
}

func newCaptureWebsocket() *captureWebsocket {
	return &captureWebsocket{
		writers:  make(chan websocket.Writer, 10),
		messages: make(chan []byte, 10),
		closed:   make(chan struct{}, 10),
	}
}

func (c *captureWebsocket) OnMessage(_ context.Context, _ *http.Request, _ websocket.Writer, _ sessions.Session, msg []byte, _ int) {
	c.messages <- msg
}

func (c *captureWebsocket) OnConnect(_ context.Context, _ *http.Request, w websocket.Writer, _ sessions.Session) func() {
	c.writers <- w
	return func() { c.closed <- struct{}{} }
}

func waitClosed(t *testing.T, ws *captureWebsocket, within time.Duration, what string) {
	t.Helper()
	select {
	case <-ws.closed:
	case <-time.After(within):
		t.Fatalf("the server did not drop %s", what)
	}
}

func TestStalledReaderIsDroppedWithoutDelayingOthers(t *testing.T) {
	t.Parallel()
	ws := newCaptureWebsocket()
	server := setupTestServer(t, ws)
	defer server.Close()

	stalled := dialWS(t, server.URL)
	defer func() { _ = stalled.CloseNow() }()
	stalledW := <-ws.writers
	healthy := dialWS(t, server.URL)
	defer func() { _ = healthy.CloseNow() }()
	healthyW := <-ws.writers

	big := make([]byte, 16*1024)
	start := time.Now()
	for range 4000 {
		stalledW.WriteMessage(websocket.Message{Type: int(coderWS.MessageText), Data: big})
	}
	assert.Less(t, time.Since(start), time.Second, "a publisher waited on a stalled client")
	waitClosed(t, ws, 15*time.Second, "the stalled client")

	healthyW.WriteMessage(websocket.Message{Type: int(coderWS.MessageText), Data: []byte("hello")})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, msg, err := healthy.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(msg))
}

func TestConcurrentPublishers(t *testing.T) {
	t.Parallel()
	ws := newCaptureWebsocket()
	server := setupTestServer(t, ws)
	defer server.Close()

	conn := dialWS(t, server.URL)
	defer func() { _ = conn.CloseNow() }()
	w := <-ws.writers

	const publishers, each = 20, 40
	var wg sync.WaitGroup
	for range publishers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				w.WriteMessage(websocket.Message{Type: int(coderWS.MessageText), Data: []byte("m")})
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := range publishers * each {
		_, msg, err := conn.Read(ctx)
		require.NoError(t, err, "message %d", i)
		require.Equal(t, "m", string(msg))
	}
	wg.Wait()
}

func TestCloseHandshake(t *testing.T) {
	t.Parallel()
	ws := newCaptureWebsocket()
	server := setupTestServer(t, ws)
	defer server.Close()

	conn := dialWS(t, server.URL)
	<-ws.writers
	assert.NoError(t, conn.Close(coderWS.StatusNormalClosure, ""), "the server must answer the close frame")
	waitClosed(t, ws, 5*time.Second, "the closed client")

	bad := dialWS(t, server.URL)
	defer func() { _ = bad.CloseNow() }()
	<-ws.writers
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, bad.Write(ctx, coderWS.MessageText, []byte{0xff, 0xfe}))
	_, _, err := bad.Read(ctx)
	assert.Equal(t, coderWS.StatusInvalidFramePayloadData, coderWS.CloseStatus(err), "invalid UTF-8 in a text frame must close the connection with 1007")
	select {
	case msg := <-ws.messages:
		t.Fatalf("invalid UTF-8 reached the handler: %x", msg)
	default:
	}
}

func TestPingTimeoutDropsSilentClient(t *testing.T) {
	t.Parallel()
	ws := newCaptureWebsocket()
	server := setupTestServer(t, ws)
	defer server.Close()

	conn := dialWS(t, server.URL)
	defer func() { _ = conn.CloseNow() }()
	<-ws.writers
	waitClosed(t, ws, 3*websocket.PingInterval(), "a client that does not answer pings")
}

func TestPingKeepsLiveClient(t *testing.T) {
	t.Parallel()
	ws := newCaptureWebsocket()
	server := setupTestServer(t, ws)
	defer server.Close()

	conn := dialWS(t, server.URL)
	defer func() { _ = conn.CloseNow() }()
	<-ws.writers
	ctx := conn.CloseRead(context.Background())
	select {
	case <-ws.closed:
		t.Fatal("the server dropped a client that answers pings")
	case <-ctx.Done():
		t.Fatal("the connection closed")
	case <-time.After(3 * websocket.PingInterval()):
	}
}
