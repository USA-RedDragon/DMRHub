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

package http_test

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestARouterClosesItsWebsocketsWhenItsContextEnds opens a websocket on a
// router made without WithWebsockets and ends the context of the router.
// The server closes the websocket with status 1001, going away.
func TestARouterClosesItsWebsocketsWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	memdb.CloseAtCleanup(t, database)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	ready := &atomic.Bool{}
	ready.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(internalhttp.CreateRouter(ctx, &cfg, nil, database, ps, ready, nil, "test", "test"))
	defer server.Close()

	conn, resp := testutils.DialWS(t, server.URL, "/ws/lastheard", testutils.CookieJar{})
	require.NotNil(t, conn, "status %d", resp.StatusCode)
	conn.WriteText(t, "ping")
	text, err := conn.ReadText(3 * time.Second)
	require.NoError(t, err)
	require.Equal(t, "PONG", text)

	cancel()
	_, err = conn.ReadText(3 * time.Second)
	var closed *testutils.WSCloseError
	require.ErrorAs(t, err, &closed)
	assert.Equal(t, 1001, closed.Code)
}
