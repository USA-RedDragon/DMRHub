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

package testutils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestWSConnAnswersPingsWhileNotReading(t *testing.T) {
	t.Parallel()
	pinged := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			pinged <- err
			return
		}
		ctx := conn.CloseRead(r.Context())
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		pinged <- conn.Ping(pingCtx)
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()

	conn, resp := DialWS(t, server.URL, "/", CookieJar{})
	require.NotNil(t, conn, "status %d", resp.StatusCode)
	select {
	case err := <-pinged:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the server never finished its ping")
	}
}
