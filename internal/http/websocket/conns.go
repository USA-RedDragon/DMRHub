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
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

const connsKey = "websocket.conns"

// Conns holds the websocket connections of a router. The HTTP server
// stops tracking a connection when its handler takes it over, so neither
// http.Server.Shutdown nor httptest.Server.Close waits for a websocket;
// Close does.
type Conns struct {
	done   chan struct{}
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

// NewConns returns an empty set of websocket connections.
func NewConns() *Conns {
	return &Conns{done: make(chan struct{})}
}

// Track is the middleware that adds each websocket request to c for the
// whole time the router handles it. After Close, it answers a websocket
// request with 503 Service Unavailable.
func (c *Conns) Track() gin.HandlerFunc {
	return func(g *gin.Context) {
		if !strings.EqualFold(g.GetHeader("Upgrade"), "websocket") {
			g.Next()
			return
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			g.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		c.wg.Add(1)
		c.mu.Unlock()
		defer c.wg.Done()
		g.Set(connsKey, c)
		g.Next()
	}
}

// Close refuses new websockets, closes each open one with status 1001,
// going away, and waits until the router has handled each to the end, or
// until ctx ends.
func (c *Conns) Close(ctx context.Context) error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
	c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("websockets still open: %w", context.Cause(ctx))
	}
}

func connsOf(g *gin.Context) *Conns {
	value, _ := g.Get(connsKey)
	c, _ := value.(*Conns)
	return c
}
