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

//go:build stress

package stress_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	apiutils "github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const accountBase = 3109500

// web serves the HTTP API of the rig's hub.
type web struct {
	router *gin.Engine
	srv    *httptest.Server
}

func (r *rig) web(t *testing.T) *web {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	ready := &atomic.Bool{}
	ready.Store(true)
	router := dmrhttp.CreateRouter(context.Background(), r.stack.Config, r.stack.Hub, r.stack.DB, r.stack.PubSub, ready, nil, "stress", "stress")
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &web{router: router, srv: srv}
}

// account makes a user that can log in, with the radio ID id.
func (w *web) account(t *testing.T, r *rig, id uint) testutils.CookieJar {
	t.Helper()
	name := fmt.Sprintf("web%d", id)
	testutils.SeedAccount(t, r.stack.DB, id, fmt.Sprintf("W%dX", id%10000), name)
	return testutils.Login(t, w.router, name)
}

// owner lets the user of rig repeater i log in, so that its websockets
// carry the calls of its radio and its repeater.
func (w *web) owner(t *testing.T, r *rig, i int) testutils.CookieJar {
	t.Helper()
	hash, err := apiutils.HashPassword(testutils.TestPassword, r.stack.Config.PasswordSalt)
	require.NoError(t, err)
	name := fmt.Sprintf("stress%d", i)
	require.NoError(t, quiet(r.stack.DB).Model(&models.User{}).Where("id = ?", userID(i)).Updates(map[string]any{"password": hash, "username": name}).Error)
	return testutils.Login(t, w.router, name)
}

type wsClient struct {
	conn     *testutils.WSConn
	msgs     atomic.Int64
	closed   atomic.Bool
	closedAt atomic.Int64
}

// TestSlowWebClients opens many last-heard and messages websockets while
// the call mix runs; some read every message and some stop reading. The
// stalled ones must be dropped, and neither the routing of bursts nor the
// clients that read may slow down.
func TestSlowWebClients(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "webclients")
	r := newRig(t, s, res, logs, rigOptions{})
	w := r.web(t)
	clients := envInt("DMRHUB_STRESS_WS_CLIENTS", 40)
	phase := max(s.Duration/2, 10*time.Second)

	r.runLanes(context.Background(), phase, loadMix(), s.Duty, 0, nil)
	r.waitLanes()
	time.Sleep(3 * time.Second)
	r.led.finish(0, true)
	base := r.led.latAll.summary()
	baseG := runtime.NumGoroutine()
	res.mu.Lock()
	res.Latency = map[string]latencySummary{"without_websockets": base}
	res.mu.Unlock()

	var all []*wsClient
	var readers sync.WaitGroup
	stop := make(chan struct{})
	start := time.Now()
	jars := map[int]testutils.CookieJar{}
	for k := range clients {
		jar, ok := jars[k%r.top.n]
		if !ok {
			jar = w.owner(t, r, k%r.top.n)
			jars[k%r.top.n] = jar
		}
		path := "/ws/calls"
		if k%2 == 1 {
			path = "/ws/messages"
		}
		if k%4 == 3 {
			all = append(all, &wsClient{conn: testutils.DialWSStalled(t, w.srv.URL, path, jar)})
			continue
		}
		conn, resp := testutils.DialWS(t, w.srv.URL, path, jar)
		if conn == nil {
			res.fail(t, "websocket %s refused: %d", path, resp.StatusCode)
			continue
		}
		c := &wsClient{conn: conn}
		all = append(all, c)
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, err := conn.ReadText(time.Second)
				if err == nil {
					c.msgs.Add(1)
					continue
				}
				if errors.Is(err, testutils.ErrWSClosed) || !isTimeout(err) {
					c.closed.Store(true)
					c.closedAt.Store(int64(time.Since(start)))
					return
				}
			}
		}()
	}
	r.led.reset()
	stall := max(phase, 50*time.Second)
	r.runLanes(context.Background(), stall, loadMix(), s.Duty, 0, nil)
	r.waitLanes()
	time.Sleep(3 * time.Second)
	r.led.finish(0, true)
	loaded := r.led.latAll.summary()
	close(stop)
	readers.Wait()
	res.mu.Lock()
	res.Latency["with_websockets"] = loaded
	res.mu.Unlock()

	dropped, stalled, readerMsgs, readersClosed := 0, 0, int64(0), 0
	for k, c := range all {
		if k%4 != 3 {
			readerMsgs += c.msgs.Load()
			if c.closed.Load() {
				readersClosed++
			}
			continue
		}
		stalled++
		if wsDropped(c.conn) {
			dropped++
		}
	}
	for _, c := range all {
		c.conn.Close()
	}
	time.Sleep(3 * time.Second)
	res.set("websockets", float64(len(all)))
	res.set("stalled_clients", float64(stalled))
	res.set("stalled_dropped", float64(dropped))
	res.set("reader_messages", float64(readerMsgs))
	res.set("readers_closed", float64(readersClosed))
	res.set("goroutines_before", float64(baseG))
	res.set("goroutines_after", float64(runtime.NumGoroutine()))
	tot, _ := r.led.snapshot()
	res.set("missing", float64(tot.Missing))
	t.Logf("websockets: %d stalled, %d dropped; readers got %d messages, %d were closed; latency %+v vs %+v", stalled, dropped, readerMsgs, readersClosed, loaded, base)
	if dropped < stalled {
		res.fail(t, "%d of %d stalled websocket clients were not dropped after %s", stalled-dropped, stalled, stall)
	}
	if readersClosed > 0 {
		res.fail(t, "%d websocket clients that read every message were closed", readersClosed)
	}
	if readerMsgs == 0 {
		res.fail(t, "the websocket clients that read got no message")
	}
	if loaded.P99 > 2*base.P99+10 {
		res.fail(t, "routing slowed down with the websockets open: p99 %.1f ms, %.1f ms without", loaded.P99, base.P99)
	}
	if tot.Missing > 0 {
		res.fail(t, "%d deliveries missing with the websockets open", tot.Missing)
	}
	if g := runtime.NumGoroutine(); g > baseG+20 {
		res.fail(t, "%d goroutines after the websockets closed, %d before they opened", g, baseG)
	}
}

func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

// wsDropped reads what the server sent to a client that stopped reading
// and reports whether the server closed the connection.
func wsDropped(c *testutils.WSConn) bool {
	for {
		_, err := c.ReadText(2 * time.Second)
		if err == nil {
			continue
		}
		return !isTimeout(err)
	}
}
