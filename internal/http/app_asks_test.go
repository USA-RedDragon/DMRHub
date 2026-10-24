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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsersMeTellsWhetherSMSStorageIsOn(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	storage := func() bool {
		t.Helper()
		w := testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/users/me", nil)
		require.Equal(t, http.StatusOK, w.Code)
		var me struct {
			SMSStorage *bool `json:"sms_storage"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &me))
		require.NotNil(t, me.SMSStorage, "users/me must report sms_storage")
		return *me.SMSStorage
	}
	assert.False(t, storage())
	w := testutils.Do(t, e.router, e.jar, http.MethodPut, "/api/v1/users/me/settings/sms-storage", map[string]bool{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, storage())

	tok := e.token(t, "talkgroups:read")
	w = e.bearer(t, http.MethodGet, "/api/v1/users/me", tok.AccessToken)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"sms_storage":true`)
}

func TestRadioIDLookupWorksWithAnAppToken(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	tok := e.token(t, "messages:read")
	w := e.bearer(t, http.MethodGet, "/api/v1/userdb/3191868", tok.AccessToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"callsign":"KI5VMF"`)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestPublicCallsWebsocketWorksFromAnySiteWithoutLogin(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	server := httptest.NewServer(e.router)
	defer server.Close()

	conn, resp := testutils.DialWSFrom(t, server.URL, "/ws/lastheard", testutils.CookieJar{}, appOrigin)
	require.NotNil(t, conn, "status %d", resp.StatusCode)
	withCookie, resp := testutils.DialWSFrom(t, server.URL, "/ws/lastheard", e.jar, appOrigin)
	require.NotNil(t, withCookie, "status %d", resp.StatusCode)
	for _, c := range []*testutils.WSConn{conn, withCookie} {
		c.WriteText(t, "PING")
		text, err := c.ReadText(3 * time.Second)
		require.NoError(t, err)
		require.Equal(t, "PONG", text)
	}

	require.NoError(t, e.tdb.PubSub().Publish("calls:public", []byte(`{"id":1}`)))
	require.NoError(t, e.tdb.PubSub().Publish("calls:1", []byte(`{"id":2,"private":true}`)))
	for _, c := range []*testutils.WSConn{conn, withCookie} {
		text, err := c.ReadText(3 * time.Second)
		require.NoError(t, err)
		assert.JSONEq(t, `{"id":1}`, text, "only the public feed, even with a session cookie from another site")
		_, err = c.ReadText(300 * time.Millisecond)
		require.Error(t, err)
	}
}
