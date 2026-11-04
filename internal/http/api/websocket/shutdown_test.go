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
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	coderWS "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestCloseDBWaitsForTheQueriesOfAnOpenCallsSocket holds a query that the
// /ws/calls socket of a user runs for a call, then ends the client and the
// server and closes the database. httptest.Server.Close does not wait for
// a websocket, so the database must not close until the query ends: a
// query that ends after the close keeps the database file open while its
// connection closes, which failed TestLogoutClosesOnlyTheSocketsOfThatSession.
func TestCloseDBWaitsForTheQueriesOfAnOpenCallsSocket(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) { memdb.Use(t, c) })
		require.NoError(t, err)
		closed := make(chan struct{})
		t.Cleanup(func() {
			select {
			case <-closed:
			default:
				tdb.CloseDB()
			}
		})
		var armed atomic.Bool
		held := make(chan struct{})
		release := make(chan struct{})
		require.NoError(t, tdb.DB().Callback().Query().Before("gorm:query").Register("test:hold", func(*gorm.DB) {
			if armed.CompareAndSwap(true, false) {
				close(held)
				<-release
			}
		}))
		const id = 1000001
		testutils.SeedAccount(t, tdb.DB(), id, "N0TEST", "user1000001")
		jar := testutils.Login(t, router, "user1000001")
		server, client := testutils.StartPipeServer(t, router)

		header := http.Header{}
		header.Set("Origin", server.URL)
		for _, c := range jar.Cookies() {
			header.Add("Cookie", c.String())
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		conn, resp, err := coderWS.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/calls", &coderWS.DialOptions{HTTPHeader: header, HTTPClient: client})
		require.NoError(t, err)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		synctest.Wait()

		armed.Store(true)
		call, err := json.Marshal(models.Call{ID: 7, StartTime: time.Now(), SourceID: otherUser, DestinationID: id, IsToUser: true, Kind: models.CallKindData})
		require.NoError(t, err)
		require.NoError(t, tdb.PubSub().Publish("calls", call))
		<-held

		_ = conn.CloseNow()
		server.Close()
		go func() {
			tdb.CloseDB()
			close(closed)
		}()
		synctest.Wait()
		select {
		case <-closed:
			close(release)
			t.Fatal("the database closed while a query of the websocket still ran")
		default:
		}
		close(release)
		<-closed
	})
}
