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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const otherUser = uint(1000099)

type userEnv struct {
	router  *gin.Engine
	db      *gorm.DB
	ps      pubsub.PubSub
	primary *httptest.Server
	replica *httptest.Server
	admin   testutils.CookieJar
	next    uint
}

func newUserEnv(t *testing.T, be testutils.Backend) *userEnv {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		c.Database.Database = fmt.Sprintf("file:wsuser_%p", t)
		c.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
		be.Migrated().Setup(t, c)
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	primary := httptest.NewServer(router)
	t.Cleanup(primary.Close)
	replica := httptest.NewServer(tdb.ReplicaRouter(t))
	t.Cleanup(replica.Close)
	testutils.SeedAccount(t, tdb.DB(), otherUser, "N0CALL", "other")
	return &userEnv{
		router:  router,
		db:      tdb.DB(),
		ps:      tdb.PubSub(),
		primary: primary,
		replica: replica,
		admin:   testutils.Login(t, router, "Admin"),
		next:    1000001,
	}
}

func (e *userEnv) user(t *testing.T, admin bool) (uint, testutils.CookieJar) {
	t.Helper()
	id := e.next
	e.next++
	name := fmt.Sprintf("user%d", id)
	testutils.SeedAccount(t, e.db, id, "N0"+name, name)
	if admin {
		require.NoError(t, e.db.Model(&models.User{}).Where("id = ?", id).Update("admin", true).Error)
	}
	return id, testutils.Login(t, e.router, name)
}

func (e *userEnv) do(t *testing.T, method, path string) {
	t.Helper()
	require.Equal(t, http.StatusOK, e.request(t, e.admin, method, path, nil), "%s %s", method, path)
}

func (e *userEnv) request(t *testing.T, jar testutils.CookieJar, method, path string, body any) int {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, e.primary.URL+path, reader)
	require.NoError(t, err)
	for _, c := range jar.Cookies() {
		req.Header.Add("Cookie", c.String())
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func (e *userEnv) login(t *testing.T, id uint) testutils.CookieJar {
	t.Helper()
	return testutils.Login(t, e.router, fmt.Sprintf("user%d", id))
}

func open(t *testing.T, server *httptest.Server, path string, jar testutils.CookieJar) *testutils.WSConn {
	t.Helper()
	conn, resp := testutils.DialWS(t, server.URL, path, jar)
	require.NotNil(t, conn, "%s: status %d", path, resp.StatusCode)
	conn.WriteText(t, "ping")
	text, err := conn.ReadText(3 * time.Second)
	require.NoError(t, err)
	require.Equal(t, "PONG", text)
	return conn
}

func privateCall(t *testing.T, id uint) []byte {
	t.Helper()
	lat, lon := 32.7767, -96.797
	call := models.Call{
		ID: 7, StartTime: time.Now(), SourceID: otherUser, DestinationID: id, IsToUser: true,
		Kind: models.CallKindVoice, Latitude: &lat, Longitude: &lon,
	}
	data, err := json.Marshal(call)
	require.NoError(t, err)
	return data
}

func (e *userEnv) requireClosed(t *testing.T, conn *testutils.WSConn, id uint, what string) {
	t.Helper()
	e.requireClosedFor(t, conn, id, "user changed", what)
}

func (e *userEnv) requireClosedFor(t *testing.T, conn *testutils.WSConn, id uint, reason, what string) {
	t.Helper()
	text, err := conn.ReadText(3 * time.Second)
	if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		require.NoError(t, e.ps.Publish("calls", privateCall(t, id)))
		got, _ := conn.ReadText(2 * time.Second)
		require.Failf(t, "socket still open", "%s: 3 s after the change the socket is open (first frame %q) and still gets %q", what, text, got)
	}
	var closed *testutils.WSCloseError
	require.ErrorAs(t, err, &closed, "%s: the server dropped the socket without a close frame", what)
	assert.Equal(t, 1008, closed.Code, what)
	assert.Equal(t, reason, closed.Reason, what)
}

func requireOpen(t *testing.T, conn *testutils.WSConn, what string) {
	t.Helper()
	conn.WriteText(t, "ping")
	text, err := conn.ReadText(3 * time.Second)
	require.NoError(t, err, "%s: a socket that should stay open was closed", what)
	require.Equal(t, "PONG", text)
}

func userBackends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

type userChange struct {
	name   string
	admin  bool
	ws     string
	method string
	path   string
}

func userChanges() []userChange {
	const suspend = "/api/v1/users/suspend/%d"
	return []userChange{
		{"demote", true, "/ws/calls", http.MethodPost, "/api/v1/users/demote/%d"},
		{"promote", false, "/ws/calls", http.MethodPost, "/api/v1/users/promote/%d"},
		{"delete", true, "/ws/calls", http.MethodDelete, "/api/v1/users/%d"},
		{"suspend", false, "/ws/calls", http.MethodPost, suspend},
		{"suspend", false, "/ws/messages", http.MethodPost, suspend},
		{"suspend", false, "/ws/nets", http.MethodPost, suspend},
		{"suspend", false, "/ws/repeaters", http.MethodPost, suspend},
		{"suspend", false, "/ws/peers", http.MethodPost, suspend},
	}
}

func TestWebsocketsCloseWhenTheUserChanges(t *testing.T) {
	t.Parallel()
	for _, be := range userBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newUserEnv(t, be)
			for _, where := range []string{"same replica", "other replica"} {
				server := e.primary
				if where == "other replica" {
					server = e.replica
				}
				for _, change := range userChanges() {
					what := fmt.Sprintf("%s on %s, %s", change.name, change.ws, where)
					id, jar := e.user(t, change.admin)
					_, bystanderJar := e.user(t, change.admin)
					conn := open(t, server, change.ws, jar)
					bystander := open(t, server, change.ws, bystanderJar)
					e.do(t, change.method, fmt.Sprintf(change.path, id))
					e.requireClosed(t, conn, id, what)
					requireOpen(t, bystander, what)
					bystander.Close()
					if change.name == "suspend" {
						again, resp := testutils.DialWS(t, server.URL, change.ws, jar)
						assert.Nil(t, again, "%s: the suspended user connected again", what)
						assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, what)
					}
				}
			}
		})
	}
}

func TestEveryUserChangeIsPublished(t *testing.T) {
	t.Parallel()
	e := newUserEnv(t, testutils.SQLiteMemoryBackend())
	changes := []struct {
		name   string
		before map[string]any
		method string
		path   string
	}{
		{"promote", nil, http.MethodPost, "/api/v1/users/promote/%d"},
		{"demote", map[string]any{"admin": true}, http.MethodPost, "/api/v1/users/demote/%d"},
		{"suspend", nil, http.MethodPost, "/api/v1/users/suspend/%d"},
		{"unsuspend", map[string]any{"suspended": true}, http.MethodPost, "/api/v1/users/unsuspend/%d"},
		{"approve", map[string]any{"approved": false}, http.MethodPost, "/api/v1/users/approve/%d"},
		{"delete", nil, http.MethodDelete, "/api/v1/users/%d"},
	}
	for _, change := range changes {
		id, _ := e.user(t, false)
		if change.before != nil {
			require.NoError(t, e.db.Model(&models.User{}).Where("id = ?", id).Updates(change.before).Error)
		}
		sub := e.ps.Subscribe(websocket.UserTopic(id))
		e.do(t, change.method, fmt.Sprintf(change.path, id))
		select {
		case <-sub.Channel():
		case <-time.After(2 * time.Second):
			assert.Fail(t, "no user change was published", change.name)
		}
		require.NoError(t, sub.Close())
	}
}

func TestRepeaterOwnershipChangesArePublished(t *testing.T) {
	t.Parallel()
	e := newUserEnv(t, testutils.SQLiteMemoryBackend())
	id, jar := e.user(t, false)
	repeaterID := id*100 + 1
	changes := []struct {
		name   string
		jar    testutils.CookieJar
		method string
		path   string
		body   any
	}{
		{"create repeater", jar, http.MethodPost, "/api/v1/repeaters", map[string]any{"id": repeaterID}},
		{"delete repeater", e.admin, http.MethodDelete, fmt.Sprintf("/api/v1/repeaters/%d", repeaterID), nil},
	}
	for _, change := range changes {
		sub := e.ps.Subscribe(websocket.UserTopic(id))
		require.Equal(t, http.StatusOK, e.request(t, change.jar, change.method, change.path, change.body), change.name)
		select {
		case <-sub.Channel():
		case <-time.After(2 * time.Second):
			assert.Fail(t, "no user change was published", change.name)
		}
		require.NoError(t, sub.Close())
	}
}
