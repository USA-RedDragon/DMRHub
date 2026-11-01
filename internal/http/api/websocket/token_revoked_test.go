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
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

const tokenOrigin = "https://script.example"

type tokenAnswer struct {
	Token struct {
		ID string `json:"id"`
	} `json:"token"`
	Secret string `json:"secret"`
}

func (e *userEnv) tokenCall(t *testing.T, jar testutils.CookieJar, method, path string, body any) tokenAnswer {
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
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s %s: %s", method, path, data)
	var out tokenAnswer
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func openWithToken(t *testing.T, server string, secret string) *testutils.WSConn {
	t.Helper()
	conn, resp := testutils.DialWSProtocols(t, server, "/ws/messages", tokenOrigin, "dmrhub", "dmrhub.bearer."+secret)
	require.NotNil(t, conn, "status %d", resp.StatusCode)
	requireOpen(t, conn, "a new token socket")
	return conn
}

func TestRevokingOrRotatingAPersonalTokenClosesItsWebsocketsOnEveryReplica(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newUserEnv(t, be)
			for _, where := range []string{"same replica", "other replica"} {
				server := e.primary
				if where == "other replica" {
					server = e.replica
				}
				for _, action := range []string{"delete", "rotate"} {
					what := fmt.Sprintf("%s on %s", action, where)
					id, jar := e.user(t, false)
					body := map[string]any{"name": "script", "scopes": []string{"messages:read"}, "expires_in_days": 30}
					revoked := e.tokenCall(t, jar, http.MethodPost, "/api/v1/users/me/tokens", body)
					kept := e.tokenCall(t, jar, http.MethodPost, "/api/v1/users/me/tokens", map[string]any{"name": "other", "full": true})
					conn := openWithToken(t, server.URL, revoked.Secret)
					bystander := openWithToken(t, server.URL, kept.Secret)
					session := open(t, server, "/ws/messages", jar)

					path := "/api/v1/users/me/tokens/" + revoked.Token.ID
					var rotated tokenAnswer
					if action == "delete" {
						e.tokenCall(t, jar, http.MethodDelete, path, nil)
					} else {
						rotated = e.tokenCall(t, jar, http.MethodPost, path+"/rotate", nil)
					}
					e.requireClosedFor(t, conn, id, "token revoked", what)
					requireOpen(t, bystander, what+": another token")
					requireOpen(t, session, what+": the signed-in session")

					again, resp := testutils.DialWSProtocols(t, server.URL, "/ws/messages", tokenOrigin, "dmrhub", "dmrhub.bearer."+revoked.Secret)
					require.Nil(t, again, "%s: the old secret opened a socket", what)
					require.Equal(t, http.StatusUnauthorized, resp.StatusCode, what)
					if action == "rotate" {
						openWithToken(t, server.URL, rotated.Secret).Close()
					}
					bystander.Close()
					session.Close()
				}
			}
		})
	}
}
