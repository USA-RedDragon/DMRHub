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

package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	app      = "https://dmr-web.example"
	callback = "https://dmr-web.example/callback"
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

func challenge() string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type env struct {
	db  *gorm.DB
	srv *oauth.Server
	now time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:oauth_%p?mode=memory&cache=shared", t)))
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.OAuthGrant{}, &models.OAuthCode{}, &models.OAuthRefreshToken{}))
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	e := &env{db: db, now: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	e.srv = oauth.NewServer(db, []byte("secret"))
	e.srv.Now = func() time.Time { return e.now }
	return e
}

func (e *env) login(t *testing.T, userID uint, scopes string) oauth.Tokens {
	t.Helper()
	req, err := e.srv.ParseAuthorize(app, callback, scopes, challenge(), "S256")
	require.NoError(t, err)
	code, err := e.srv.Approve(userID, req)
	require.NoError(t, err)
	tok, err := e.srv.Exchange(code, app, callback, verifier)
	require.NoError(t, err)
	return tok
}

func TestClientIsAnOriginAndRedirectsOnlyToIt(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, tc := range []struct {
		client, redirect string
		ok               bool
	}{
		{app, callback, true},
		{"http://localhost:5173", "http://localhost:5173/", true},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/cb", true},
		{"http://dmr-web.example", "http://dmr-web.example/cb", false},
		{app + "/", callback, false},
		{app, "https://evil.example/callback", false},
		{app, "https://dmr-web.example.evil.example/cb", false},
		{app, callback + "#frag", false},
		{"https://user@dmr-web.example", "https://user@dmr-web.example/cb", false},
		{"", callback, false},
	} {
		_, err := e.srv.ParseAuthorize(tc.client, tc.redirect, "talkgroups:read", challenge(), "S256")
		if tc.ok {
			assert.NoError(t, err, "%s -> %s", tc.client, tc.redirect)
		} else {
			assert.Error(t, err, "%s -> %s", tc.client, tc.redirect)
		}
	}
}

func TestAuthorizeNeedsKnownScopesAndS256(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, err := e.srv.ParseAuthorize(app, callback, "talkgroups:read admin", challenge(), "S256")
	require.ErrorIs(t, err, oauth.ErrInvalidScope)
	_, err = e.srv.ParseAuthorize(app, callback, "", challenge(), "S256")
	require.ErrorIs(t, err, oauth.ErrInvalidScope)
	_, err = e.srv.ParseAuthorize(app, callback, "talkgroups:read", verifier, "plain")
	require.ErrorIs(t, err, oauth.ErrInvalidRequest)
	_, err = e.srv.ParseAuthorize(app, callback, "talkgroups:read", "short", "S256")
	require.ErrorIs(t, err, oauth.ErrInvalidRequest)

	req, err := e.srv.ParseAuthorize(app, callback, "messages:read talkgroups:read messages:read", challenge(), "S256")
	require.NoError(t, err)
	names := make([]string, 0, len(req.Scopes))
	for _, s := range req.Scopes {
		names = append(names, s.Name)
		assert.NotEmpty(t, s.Description)
	}
	assert.Equal(t, []string{"talkgroups:read", "messages:read"}, names)
}

func TestCodeWorksOnceWithItsVerifierAndRedirect(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	req, err := e.srv.ParseAuthorize(app, callback, "talkgroups:read", challenge(), "S256")
	require.NoError(t, err)

	code, err := e.srv.Approve(7, req)
	require.NoError(t, err)
	_, err = e.srv.Exchange(code, app, callback, "wrong-verifier-wrong-verifier-wrong-verifier")
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)

	code, err = e.srv.Approve(7, req)
	require.NoError(t, err)
	_, err = e.srv.Exchange(code, app, "https://dmr-web.example/other", verifier)
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)

	code, err = e.srv.Approve(7, req)
	require.NoError(t, err)
	_, err = e.srv.Exchange(code, "https://other.example", callback, verifier)
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)

	code, err = e.srv.Approve(7, req)
	require.NoError(t, err)
	tok, err := e.srv.Exchange(code, app, callback, verifier)
	require.NoError(t, err)
	assert.NotEmpty(t, tok.Access)
	assert.NotEmpty(t, tok.Refresh)
	assert.Equal(t, oauth.AccessLifetime, tok.ExpiresIn)
	assert.Equal(t, "talkgroups:read", tok.Scope)
	_, err = e.srv.Exchange(code, app, callback, verifier)
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)

	code, err = e.srv.Approve(7, req)
	require.NoError(t, err)
	e.now = e.now.Add(oauth.CodeLifetime + time.Second)
	_, err = e.srv.Exchange(code, app, callback, verifier)
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
}

func TestAccessTokenNamesTheUserAndScopesUntilItExpires(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tok := e.login(t, 7, "talkgroups:read lastheard:read")

	grant, err := e.srv.Authenticate(tok.Access)
	require.NoError(t, err)
	assert.Equal(t, uint(7), grant.UserID)
	assert.True(t, grant.Allows("lastheard:read"))
	assert.False(t, grant.Allows("messages:read"))

	_, err = e.srv.Authenticate(tok.Access + "x")
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
	_, err = e.srv.Authenticate(tok.Refresh)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
	other := oauth.NewServer(e.db, []byte("other secret"))
	other.Now = e.srv.Now
	_, err = other.Authenticate(tok.Access)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)

	e.now = e.now.Add(oauth.AccessLifetime + time.Second)
	_, err = e.srv.Authenticate(tok.Access)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
}

func TestRefreshRotatesAndReuseEndsTheGrant(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	first := e.login(t, 7, "talkgroups:read")

	e.now = e.now.Add(20 * time.Minute)
	second, err := e.srv.Refresh(first.Refresh, app)
	require.NoError(t, err)
	assert.NotEqual(t, first.Refresh, second.Refresh)
	_, err = e.srv.Authenticate(second.Access)
	require.NoError(t, err)

	_, err = e.srv.Refresh(second.Refresh, "https://other.example")
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)

	_, err = e.srv.Refresh(first.Refresh, app)
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
	_, err = e.srv.Refresh(second.Refresh, app)
	require.ErrorIs(t, err, oauth.ErrInvalidGrant, "reuse of a spent refresh token ends the grant")
	_, err = e.srv.Authenticate(second.Access)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
}

func TestRefreshTokenExpires(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tok := e.login(t, 7, "talkgroups:read")
	e.now = e.now.Add(oauth.RefreshLifetime + time.Second)
	_, err := e.srv.Refresh(tok.Refresh, app)
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
}

func TestRevokeEndsTheGrantAndUnknownTokensAreFine(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tok := e.login(t, 7, "talkgroups:read")
	require.NoError(t, e.srv.Revoke("not a token", app))
	require.NoError(t, e.srv.Revoke(tok.Refresh, "https://other.example"))
	_, err := e.srv.Authenticate(tok.Access)
	require.NoError(t, err, "another app cannot revoke this one")

	require.NoError(t, e.srv.Revoke(tok.Refresh, app))
	_, err = e.srv.Authenticate(tok.Access)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
	_, err = e.srv.Refresh(tok.Refresh, app)
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
}

func TestAnAppHasOneGrantPerUserThatTheUserCanRemove(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	old := e.login(t, 7, "talkgroups:read")
	e.now = e.now.Add(time.Minute)
	tok := e.login(t, 7, "talkgroups:read messages:read")
	e.login(t, 8, "talkgroups:read")

	grants, err := e.srv.Grants(7)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, app, grants[0].ClientID)
	assert.Equal(t, "talkgroups:read messages:read", grants[0].Scopes)
	_, err = e.srv.Authenticate(old.Access)
	require.NoError(t, err, "authorizing again keeps the grant")

	require.ErrorIs(t, e.srv.RemoveGrant(8, grants[0].ID), oauth.ErrNotFound)
	require.NoError(t, e.srv.RemoveGrant(7, grants[0].ID))
	_, err = e.srv.Authenticate(tok.Access)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
	grants, err = e.srv.Grants(7)
	require.NoError(t, err)
	assert.Empty(t, grants)
	grants, err = e.srv.Grants(8)
	require.NoError(t, err)
	assert.Len(t, grants, 1)
}

func TestSummaryNamesWhatAnAppCanUse(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Talkgroups, last heard, messages, hotspots", oauth.Summary("hotspots:register messages:read talkgroups:read hotspots:read lastheard:read"))
	assert.Equal(t, "Hotspot talkgroups", oauth.Summary("hotspots:talkgroups"))
}
