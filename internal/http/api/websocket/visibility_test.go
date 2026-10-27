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
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	apiWebsocket "github.com/USA-RedDragon/DMRHub/internal/http/api/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicCallsFollowTheVisibility(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	current := config.NewCurrent(cfg)
	ps, err := pubsub.MakePubSub(context.Background(), cfg)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("sessions", cookie.NewStore([]byte("test-secret"))))
	router.GET("/ws", websocket.CreatePublicHandler(cfg, apiWebsocket.CreatePublicCallsWebsocket(current, ps)))
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dial(t, server.URL)
	defer func() { _ = conn.CloseNow() }()
	time.Sleep(100 * time.Millisecond)

	lat := 32.7767
	sent, err := json.Marshal(apimodels.WSCallResponse{ID: 1, TalkerAlias: "KI5VMF Jacob", Grid: "EM12", Latitude: &lat, Longitude: &lat})
	require.NoError(t, err)
	read := func() apimodels.WSCallResponse {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, msg, err := conn.Read(ctx)
		require.NoError(t, err)
		var call apimodels.WSCallResponse
		require.NoError(t, json.Unmarshal(msg, &call))
		return call
	}

	require.NoError(t, ps.Publish("calls:public", sent))
	assert.Equal(t, "KI5VMF Jacob", read().TalkerAlias)

	hidden := *cfg
	hidden.Visibility = config.VisibilityHideNames
	current.Set(&hidden)
	require.NoError(t, ps.Publish("calls:public", sent))
	call := read()
	assert.Equal(t, uint(1), call.ID)
	assert.Empty(t, call.TalkerAlias)
	assert.Empty(t, call.Grid)
	assert.Nil(t, call.Latitude)
}
