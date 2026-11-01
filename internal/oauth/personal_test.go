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
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPersonalTokenIsShownOnceAndOnlyItsHashIsKept(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tok, secret, err := e.srv.CreatePersonal(7, oauth.PersonalRequest{Name: " Net logger ", Scopes: "lastheard:read talkgroups:read", Days: 90})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(secret, oauth.PersonalPrefix), secret)
	assert.Greater(t, len(secret), len(oauth.PersonalPrefix)+40)
	assert.Equal(t, "Net logger", tok.Name)
	assert.Equal(t, "talkgroups:read lastheard:read", tok.Scopes)
	assert.False(t, tok.Full)
	assert.Equal(t, secret[:len(oauth.PersonalPrefix)+4], tok.Prefix)
	require.NotNil(t, tok.ExpiresAt)
	assert.Equal(t, e.now.Add(90*24*time.Hour), *tok.ExpiresAt)

	var rows []models.PersonalToken
	require.NoError(t, e.db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.NotContains(t, rows[0].Hash, secret[len(oauth.PersonalPrefix):])
	assert.Len(t, rows[0].Hash, 64)
	for _, field := range []string{rows[0].ID, rows[0].Name, rows[0].Prefix, rows[0].Scopes} {
		assert.NotContains(t, field, secret[len(oauth.PersonalPrefix)+4:])
	}

	got, err := e.srv.AuthenticatePersonal(secret)
	require.NoError(t, err)
	assert.Equal(t, tok.ID, got.ID)
	assert.Equal(t, uint(7), got.UserID)
	g := got.Grant()
	assert.Equal(t, uint(7), g.UserID)
	assert.True(t, g.Allows(oauth.ScopeLastHeardRead))
	assert.False(t, g.Allows(oauth.ScopeMessagesRead))
	assert.NotEqual(t, tok.ID, g.ID, "the websocket key of a personal token must not clash with an app grant ID")
}

func TestAPersonalTokenNeedsANameAKnownExpiryAndScopesOrFullAccess(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, tc := range []struct {
		name string
		req  oauth.PersonalRequest
		err  error
	}{
		{"no name", oauth.PersonalRequest{Name: "  ", Scopes: "talkgroups:read", Days: 30}, oauth.ErrInvalidRequest},
		{"long name", oauth.PersonalRequest{Name: strings.Repeat("x", 65), Scopes: "talkgroups:read", Days: 30}, oauth.ErrInvalidRequest},
		{"odd expiry", oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read", Days: 31}, oauth.ErrInvalidRequest},
		{"negative expiry", oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read", Days: -1}, oauth.ErrInvalidRequest},
		{"no scope", oauth.PersonalRequest{Name: "a", Days: 30}, oauth.ErrInvalidScope},
		{"unknown scope", oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read admin", Days: 30}, oauth.ErrInvalidScope},
		{"full and scopes", oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read", Full: true, Days: 30}, oauth.ErrInvalidRequest},
	} {
		_, _, err := e.srv.CreatePersonal(1, tc.req)
		require.ErrorIs(t, err, tc.err, tc.name)
	}
	for _, days := range []int{0, 30, 90, 365} {
		tok, _, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "ok", Full: true, Days: days})
		require.NoError(t, err, days)
		assert.True(t, tok.Full)
		assert.Empty(t, tok.Scopes)
		if days == 0 {
			assert.Nil(t, tok.ExpiresAt, "no expiry")
		} else {
			require.NotNil(t, tok.ExpiresAt)
			assert.Equal(t, e.now.AddDate(0, 0, days), *tok.ExpiresAt)
		}
	}
}

func TestAFullAccessTokenAllowsEveryScope(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, secret, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "all", Full: true})
	require.NoError(t, err)
	tok, err := e.srv.AuthenticatePersonal(secret)
	require.NoError(t, err)
	g := tok.Grant()
	assert.True(t, g.Full)
	for _, sc := range oauth.Scopes() {
		assert.True(t, g.Allows(sc.Name), sc.Name)
	}
}

func TestAPersonalTokenWithAnotherPrefixOrSecretIsRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, secret, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read"})
	require.NoError(t, err)
	body := strings.TrimPrefix(secret, oauth.PersonalPrefix)
	for _, bad := range []string{
		body,
		"dmrhub_at." + body,
		"dmrhub_rt." + body,
		"DMRHUB_PAT_" + body,
		oauth.PersonalPrefix,
		oauth.PersonalPrefix + body[:len(body)-1],
		oauth.PersonalPrefix + body + "x",
		"",
	} {
		_, err := e.srv.AuthenticatePersonal(bad)
		require.ErrorIs(t, err, oauth.ErrInvalidToken, "%q", bad)
	}
}

func TestAPersonalTokenStopsAtItsExpiry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, secret, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read", Days: 30})
	require.NoError(t, err)
	e.now = e.now.AddDate(0, 0, 30).Add(-time.Second)
	_, err = e.srv.AuthenticatePersonal(secret)
	require.NoError(t, err)
	e.now = e.now.Add(time.Second)
	_, err = e.srv.AuthenticatePersonal(secret)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)

	tokens, err := e.srv.PersonalTokens(1)
	require.NoError(t, err)
	require.Len(t, tokens, 1, "an expired token stays in the list until the user deletes or rotates it")
}

func TestAPersonalTokenWithoutExpiryKeepsWorking(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, secret, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read"})
	require.NoError(t, err)
	e.now = e.now.AddDate(20, 0, 0)
	_, err = e.srv.AuthenticatePersonal(secret)
	require.NoError(t, err)
}

func TestUsingAPersonalTokenRecordsWhenItWasLastUsed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tok, secret, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read"})
	require.NoError(t, err)
	assert.Nil(t, tok.LastUsedAt)
	e.now = e.now.Add(time.Hour)
	_, err = e.srv.AuthenticatePersonal(secret)
	require.NoError(t, err)
	tokens, err := e.srv.PersonalTokens(1)
	require.NoError(t, err)
	require.NotNil(t, tokens[0].LastUsedAt)
	assert.Equal(t, e.now, tokens[0].LastUsedAt.UTC())
}

func TestDeletingAPersonalTokenEndsItAndOnlyForItsUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tok, secret, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read"})
	require.NoError(t, err)
	require.ErrorIs(t, e.srv.RemovePersonal(2, tok.ID), oauth.ErrNotFound)
	_, err = e.srv.AuthenticatePersonal(secret)
	require.NoError(t, err, "another user cannot delete the token")
	require.NoError(t, e.srv.RemovePersonal(1, tok.ID))
	_, err = e.srv.AuthenticatePersonal(secret)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
	require.ErrorIs(t, e.srv.RemovePersonal(1, tok.ID), oauth.ErrNotFound)
}

func TestRotatingAPersonalTokenKeepsItsNameScopesAndExpiryAndEndsTheOldSecret(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	old, oldSecret, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "Net logger", Scopes: "messages:read positions:read", Days: 30})
	require.NoError(t, err)
	e.now = e.now.AddDate(0, 0, 40)
	_, err = e.srv.AuthenticatePersonal(oldSecret)
	require.ErrorIs(t, err, oauth.ErrInvalidToken, "expired before the rotation")

	_, _, err = e.srv.RotatePersonal(2, old.ID)
	require.ErrorIs(t, err, oauth.ErrNotFound, "another user cannot rotate the token")

	rotated, secret, err := e.srv.RotatePersonal(1, old.ID)
	require.NoError(t, err)
	assert.NotEqual(t, oldSecret, secret)
	assert.NotEqual(t, old.ID, rotated.ID)
	assert.Equal(t, old.Name, rotated.Name)
	assert.Equal(t, old.Scopes, rotated.Scopes)
	assert.Equal(t, old.Full, rotated.Full)
	assert.Equal(t, 30, rotated.Days)
	require.NotNil(t, rotated.ExpiresAt)
	assert.Equal(t, e.now.AddDate(0, 0, 30), *rotated.ExpiresAt)

	_, err = e.srv.AuthenticatePersonal(oldSecret)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
	got, err := e.srv.AuthenticatePersonal(secret)
	require.NoError(t, err)
	assert.Equal(t, rotated.ID, got.ID)
	tokens, err := e.srv.PersonalTokens(1)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Equal(t, rotated.ID, tokens[0].ID)

	_, _, err = e.srv.RotatePersonal(1, old.ID)
	require.ErrorIs(t, err, oauth.ErrNotFound, "the old token is gone")
}

func TestRotatingAFullAccessTokenWithoutExpiryKeepsBoth(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	old, _, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "all", Full: true})
	require.NoError(t, err)
	rotated, _, err := e.srv.RotatePersonal(1, old.ID)
	require.NoError(t, err)
	assert.True(t, rotated.Full)
	assert.Nil(t, rotated.ExpiresAt)
}

func TestAUserHasAtMostSoManyPersonalTokens(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for range oauth.MaxPersonalTokens {
		_, _, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read"})
		require.NoError(t, err)
	}
	_, _, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read"})
	require.ErrorIs(t, err, oauth.ErrTooManyTokens)
	_, _, err = e.srv.CreatePersonal(2, oauth.PersonalRequest{Name: "a", Scopes: "talkgroups:read"})
	require.NoError(t, err)
}

func TestAPasswordResetEndsThePersonalTokensOfItsUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, mine, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: "a", Full: true})
	require.NoError(t, err)
	_, other, err := e.srv.CreatePersonal(2, oauth.PersonalRequest{Name: "a", Full: true})
	require.NoError(t, err)
	require.NoError(t, e.srv.RevokeUserGrants(1))
	_, err = e.srv.AuthenticatePersonal(mine)
	require.ErrorIs(t, err, oauth.ErrInvalidToken)
	_, err = e.srv.AuthenticatePersonal(other)
	require.NoError(t, err)
}

func TestPersonalTokensListInTheOrderTheyWereMade(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, name := range []string{"first", "second", "third"} {
		_, _, err := e.srv.CreatePersonal(1, oauth.PersonalRequest{Name: name, Scopes: "talkgroups:read"})
		require.NoError(t, err)
		e.now = e.now.Add(time.Minute)
	}
	tokens, err := e.srv.PersonalTokens(1)
	require.NoError(t, err)
	require.Len(t, tokens, 3)
	assert.Equal(t, []string{"first", "second", "third"}, []string{tokens[0].Name, tokens[1].Name, tokens[2].Name})
	none, err := e.srv.PersonalTokens(2)
	require.NoError(t, err)
	assert.Empty(t, none)
}
