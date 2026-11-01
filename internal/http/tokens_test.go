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
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type personalToken struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Prefix   string   `json:"prefix"`
	Scopes   []string `json:"scopes"`
	Full     bool     `json:"full"`
	CanUse   string   `json:"can_use"`
	Days     int      `json:"days"`
	Made     string   `json:"made"`
	LastUsed *string  `json:"last_used"`
	Expires  *string  `json:"expires"`
	Expired  bool     `json:"expired"`
}

type madeToken struct {
	Token  personalToken `json:"token"`
	Secret string        `json:"secret"`
}

func makeToken(t *testing.T, e *oauthEnv, jar testutils.CookieJar, body map[string]any) madeToken {
	t.Helper()
	w := testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/tokens", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var out madeToken
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.NotEmpty(t, out.Secret)
	return out
}

func TestAPersonalTokenIsMadeListedAndShownOnce(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	made := makeToken(t, e, e.jar, map[string]any{"name": "Net logger", "scopes": []string{"talkgroups:read", "lastheard:read"}, "expires_in_days": 90})
	assert.Equal(t, "Net logger", made.Token.Name)
	assert.Equal(t, []string{"talkgroups:read", "lastheard:read"}, made.Token.Scopes)
	assert.Equal(t, "Talkgroups, last heard", made.Token.CanUse)
	assert.True(t, strings.HasPrefix(made.Secret, made.Token.Prefix))
	require.NotNil(t, made.Token.Expires)

	w := testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/users/me/tokens", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), made.Secret[len(made.Token.Prefix):])
	keys := testutils.JSONKeys(t, w.Body.Bytes())
	assert.False(t, keys["secret"])
	assert.False(t, keys["hash"])
	var list struct {
		Tokens   []personalToken                             `json:"tokens"`
		Scopes   []struct{ Name, Label, Description string } `json:"scopes"`
		Expiries []int                                       `json:"expiries"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Tokens, 1)
	assert.Equal(t, made.Token.ID, list.Tokens[0].ID)
	assert.Nil(t, list.Tokens[0].LastUsed)
	assert.Len(t, list.Scopes, 7)
	assert.Equal(t, []int{30, 90, 365, 0}, list.Expiries)

	full := makeToken(t, e, e.jar, map[string]any{"name": "Everything", "full": true, "expires_in_days": 0})
	assert.True(t, full.Token.Full)
	assert.Nil(t, full.Token.Expires)
	assert.Equal(t, "Full account access", full.Token.CanUse)

	for _, bad := range []map[string]any{
		{"name": "", "scopes": []string{"talkgroups:read"}, "expires_in_days": 30},
		{"name": "x", "scopes": []string{}, "expires_in_days": 30},
		{"name": "x", "scopes": []string{"admin"}, "expires_in_days": 30},
		{"name": "x", "scopes": []string{"talkgroups:read"}, "expires_in_days": 7},
		{"name": "x", "scopes": []string{"talkgroups:read"}, "full": true, "expires_in_days": 30},
	} {
		w := testutils.Do(t, e.router, e.jar, http.MethodPost, "/api/v1/users/me/tokens", bad)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%v: %s", bad, w.Body.String())
	}
}

func TestAPersonalTokenActsAsItsUserWithinItsScopes(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	made := makeToken(t, e, e.jar, map[string]any{"name": "a", "scopes": []string{"talkgroups:read"}, "expires_in_days": 30})

	w := e.bearer(t, http.MethodGet, "/api/v1/users/me", made.Secret)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"username":"Admin"`)
	assert.Empty(t, w.Header().Get("Set-Cookie"))
	assert.Equal(t, http.StatusOK, e.bearer(t, http.MethodGet, "/api/v1/talkgroups/my", made.Secret).Code)

	w = e.bearer(t, http.MethodGet, "/api/v1/messages/inbox", made.Secret)
	assert.Equal(t, http.StatusForbidden, w.Code, "a scope outside the token's list")
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`)
	assert.Equal(t, http.StatusForbidden, e.bearer(t, http.MethodGet, "/api/v1/users/me/settings", made.Secret).Code, "a route of no scope")
	assert.Equal(t, http.StatusForbidden, e.bearer(t, http.MethodGet, "/api/v1/repeaters", made.Secret).Code)

	tokens := testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/users/me/tokens", nil)
	var list struct {
		Tokens []personalToken `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal(tokens.Body.Bytes(), &list))
	require.NotNil(t, list.Tokens[0].LastUsed, "using the token records it")
}

func TestABrokenPersonalTokenIsRefused(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	made := makeToken(t, e, e.jar, map[string]any{"name": "a", "full": true, "expires_in_days": 0})
	body := strings.TrimPrefix(made.Secret, "dmrhub_pat_")
	for _, bad := range []string{
		body,
		"dmrhub_at." + body,
		"dmrhub_pa_" + body,
		"dmrhub_pat_" + body[1:],
		"dmrhub_pat_" + strings.ToUpper(body),
	} {
		w := e.bearer(t, http.MethodGet, "/api/v1/users/me", bad)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%q", bad)
		assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
	}
}

func TestAnExpiredPersonalTokenIsRefusedAndListedAsExpired(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	made := makeToken(t, e, e.jar, map[string]any{"name": "a", "scopes": []string{"talkgroups:read"}, "expires_in_days": 30})
	require.Equal(t, http.StatusOK, e.bearer(t, http.MethodGet, "/api/v1/users/me", made.Secret).Code)
	past := time.Now().Add(-time.Minute)
	require.NoError(t, e.tdb.DB().Model(&models.PersonalToken{}).Where("id = ?", made.Token.ID).Update("expires_at", past).Error)

	w := e.bearer(t, http.MethodGet, "/api/v1/users/me", made.Secret)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)

	w = testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/users/me/tokens", nil)
	var list struct {
		Tokens []personalToken `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Tokens, 1)
	assert.True(t, list.Tokens[0].Expired)
}

func TestADeletedPersonalTokenIsRefused(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	made := makeToken(t, e, e.jar, map[string]any{"name": "a", "scopes": []string{"talkgroups:read"}, "expires_in_days": 30})
	other := testutils.SeedAccount(t, e.tdb.DB(), 1000099, "N0CALL", "other")
	otherJar := testutils.Login(t, e.router, other.Username)
	assert.Equal(t, http.StatusNotFound, testutils.Do(t, e.router, otherJar, http.MethodDelete, "/api/v1/users/me/tokens/"+made.Token.ID, nil).Code)
	assert.Equal(t, http.StatusNotFound, testutils.Do(t, e.router, otherJar, http.MethodPost, "/api/v1/users/me/tokens/"+made.Token.ID+"/rotate", nil).Code)
	require.Equal(t, http.StatusOK, e.bearer(t, http.MethodGet, "/api/v1/users/me", made.Secret).Code)

	w := testutils.Do(t, e.router, e.jar, http.MethodDelete, "/api/v1/users/me/tokens/"+made.Token.ID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, http.StatusUnauthorized, e.bearer(t, http.MethodGet, "/api/v1/users/me", made.Secret).Code)
	assert.Equal(t, http.StatusNotFound, testutils.Do(t, e.router, e.jar, http.MethodDelete, "/api/v1/users/me/tokens/"+made.Token.ID, nil).Code)
}

func TestARotatedPersonalTokenRefusesTheOldSecret(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	made := makeToken(t, e, e.jar, map[string]any{"name": "Net logger", "scopes": []string{"messages:read"}, "expires_in_days": 365})

	w := testutils.Do(t, e.router, e.jar, http.MethodPost, "/api/v1/users/me/tokens/"+made.Token.ID+"/rotate", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var rotated madeToken
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rotated))
	assert.NotEqual(t, made.Secret, rotated.Secret)
	assert.Equal(t, "Net logger", rotated.Token.Name)
	assert.Equal(t, []string{"messages:read"}, rotated.Token.Scopes)
	assert.Equal(t, 365, rotated.Token.Days)

	w = e.bearer(t, http.MethodGet, "/api/v1/messages/inbox", made.Secret)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "the old secret")
	w = e.bearer(t, http.MethodGet, "/api/v1/messages/inbox", rotated.Secret)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAFullAccessTokenHasTheRightsOfItsUserAndNeverMore(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	plain := testutils.SeedAccount(t, e.tdb.DB(), 1000099, "N0CALL", "plain")
	plainJar := testutils.Login(t, e.router, plain.Username)
	for _, who := range []struct {
		name string
		jar  testutils.CookieJar
		id   uint
	}{
		{"super admin", e.jar, 0},
		{"user", plainJar, plain.ID},
	} {
		if who.id == 0 {
			var admin models.User
			require.NoError(t, e.tdb.DB().Where("username = ?", "Admin").Take(&admin).Error)
			who.id = admin.ID
		}
		made := makeToken(t, e, who.jar, map[string]any{"name": "all", "full": true, "expires_in_days": 0})
		for _, route := range []string{"/api/v1/users/me/settings", "/api/v1/messages/inbox", "/api/v1/repeaters/my", "/api/v1/users/me/radio-ids"} {
			w := e.bearer(t, http.MethodGet, route, made.Secret)
			assert.Equal(t, http.StatusOK, w.Code, "%s: %s: %s", who.name, route, w.Body.String())
		}
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/repeaters"},
			{http.MethodGet, "/api/v1/admin/messages"},
			{http.MethodGet, "/api/v1/config"},
			{http.MethodGet, "/api/v1/users/suspended"},
			{http.MethodGet, "/api/v1/users/admins"},
			{http.MethodPost, "/api/v1/talkgroups"},
		} {
			w := e.bearer(t, route.method, route.path, made.Secret)
			assert.Equal(t, http.StatusForbidden, w.Code, "%s: admin-only %s %s: %s", who.name, route.method, route.path, w.Body.String())
		}
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/users/me/tokens"},
			{http.MethodPost, "/api/v1/users/me/tokens"},
			{http.MethodDelete, "/api/v1/users/me/tokens/" + made.Token.ID},
			{http.MethodPost, "/api/v1/users/me/tokens/" + made.Token.ID + "/rotate"},
			{http.MethodGet, "/api/v1/oauth/apps"},
			{http.MethodDelete, "/api/v1/oauth/apps/x"},
			{http.MethodPost, "/api/v1/oauth/authorize"},
			{http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", who.id)},
			{http.MethodPut, "/api/v1/users/me/email"},
			{http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", who.id)},
			{http.MethodPost, "/api/v1/auth/logout"},
		} {
			w := e.bearer(t, route.method, route.path, made.Secret)
			assert.Equal(t, http.StatusForbidden, w.Code, "%s: account security %s %s: %s", who.name, route.method, route.path, w.Body.String())
			assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`, "%s %s", route.method, route.path)
		}
		require.Equal(t, http.StatusOK, e.bearer(t, http.MethodGet, "/api/v1/users/me", made.Secret).Code, "%s: the token survived its own refused calls", who.name)
	}
}

func TestAPersonalTokenCannotManageTokens(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	made := makeToken(t, e, e.jar, map[string]any{"name": "a", "scopes": []string{"talkgroups:read", "lastheard:read", "messages:read", "positions:read", "hotspots:read", "hotspots:register", "hotspots:talkgroups"}, "expires_in_days": 30})
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/me/tokens"},
		{http.MethodPost, "/api/v1/users/me/tokens"},
		{http.MethodPost, "/api/v1/users/me/tokens/" + made.Token.ID + "/rotate"},
		{http.MethodDelete, "/api/v1/users/me/tokens/" + made.Token.ID},
	} {
		assert.Equal(t, http.StatusForbidden, e.bearer(t, route.method, route.path, made.Secret).Code, "%s %s", route.method, route.path)
	}
}
