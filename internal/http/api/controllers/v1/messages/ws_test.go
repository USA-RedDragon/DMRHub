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

package messages_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func push(t *testing.T, conn *testutils.WSConn) apimodels.MessagePush {
	t.Helper()
	text, err := conn.ReadText(3 * time.Second)
	require.NoError(t, err)
	var f apimodels.MessagePush
	require.NoError(t, json.Unmarshal([]byte(text), &f))
	return f
}

func noPush(t *testing.T, conn *testutils.WSConn) {
	t.Helper()
	text, err := conn.ReadText(300 * time.Millisecond)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "got %q", text)
}

func TestMessagesWebsocketAnswersPing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	server := httptest.NewServer(e.router)
	defer server.Close()

	conn, resp := testutils.DialWS(t, server.URL, "/ws/messages", e.jar)
	require.NotNil(t, conn, "status %d", resp.StatusCode)
	conn.WriteText(t, "PING")
	text, err := conn.ReadText(3 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, "PONG", text)
}

func TestMessagesWebsocketNeedsLogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	server := httptest.NewServer(e.router)
	defer server.Close()

	conn, resp := testutils.DialWS(t, server.URL, "/ws/messages", testutils.CookieJar{})
	assert.Nil(t, conn)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMessagesWebsocketPushesOnlyVisibleMessagesToEachUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	server := httptest.NewServer(e.router)
	defer server.Close()
	since := e.storage(true)
	otherJar := testutils.Login(t, e.router, "other")
	w := testutils.Do(t, e.router, otherJar, http.MethodPut, "/api/v1/users/me/settings/sms-storage", map[string]bool{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code)

	mine, _ := testutils.DialWS(t, server.URL, "/ws/messages", e.jar)
	theirs, _ := testutils.DialWS(t, server.URL, "/ws/messages", otherJar)
	require.NotNil(t, mine)
	require.NotNil(t, theirs)
	mine.WriteText(t, "ping")
	_, err := mine.ReadText(3 * time.Second)
	require.NoError(t, err)
	theirs.WriteText(t, "ping")
	_, err = theirs.ReadText(3 * time.Second)
	require.NoError(t, err)

	pusher := messages.NewPusher(e.db, e.ps)
	toMe := e.msg(time.Now(), uint(1000099), owner, false, "for owner")
	pusher.Push(&toMe)
	f := push(t, mine)
	assert.Equal(t, []string{"inbox"}, f.Boxes)
	assert.Equal(t, toMe.ID, f.Message.ID)
	noPush(t, theirs)

	toOther := e.msg(time.Now(), uint(1000099), other, false, "for other")
	pusher.Push(&toOther)
	f = push(t, theirs)
	assert.Equal(t, toOther.ID, f.Message.ID)
	noPush(t, mine)

	between := e.msg(time.Now(), owner, other, false, "between us")
	pusher.Push(&between)
	assert.Equal(t, []string{"outbox"}, push(t, mine).Boxes)
	assert.Equal(t, []string{"inbox"}, push(t, theirs).Boxes)

	old := e.msg(since.Add(-time.Minute), uint(1000099), owner, false, "before the opt-in")
	pusher.Push(&old)
	noPush(t, mine)

	theirs.Close()
	again := e.msg(time.Now(), uint(1000099), owner, false, "still here")
	pusher.Push(&again)
	assert.Equal(t, again.ID, push(t, mine).Message.ID, "closing one connection leaves the other")

	e.storage(false)
	off := e.msg(time.Now(), uint(1000099), owner, false, "storage off")
	pusher.Push(&off)
	noPush(t, mine)

	var n int64
	require.NoError(t, e.db.Model(&models.Message{}).Count(&n).Error)
	assert.Equal(t, int64(6), n)
}
