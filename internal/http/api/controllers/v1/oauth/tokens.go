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

package oauth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-gonic/gin"
)

const (
	fullAccessLabel = "Full account access"
	msgTryAgain     = "Try again later"
)

// PersonalToken is a personal API token of the user, as the Settings page
// lists it. It never holds the secret.
type PersonalToken struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Prefix   string     `json:"prefix"`
	Scopes   []string   `json:"scopes"`
	Full     bool       `json:"full"`
	CanUse   string     `json:"can_use"`
	Days     int        `json:"days"`
	Made     time.Time  `json:"made"`
	LastUsed *time.Time `json:"last_used"`
	Expires  *time.Time `json:"expires"`
	Expired  bool       `json:"expired"`
}

func personalToken(t models.PersonalToken, now time.Time) PersonalToken {
	out := PersonalToken{
		ID:       t.ID,
		Name:     t.Name,
		Prefix:   t.Prefix,
		Scopes:   strings.Fields(t.Scopes),
		Full:     t.Full,
		CanUse:   oauth.Summary(t.Scopes),
		Days:     t.Days,
		Made:     t.CreatedAt,
		LastUsed: t.LastUsedAt,
		Expires:  t.ExpiresAt,
		Expired:  t.ExpiresAt != nil && !now.Before(*t.ExpiresAt),
	}
	if out.Scopes == nil {
		out.Scopes = []string{}
	}
	if t.Full {
		out.CanUse = fullAccessLabel
	}
	return out
}

// PersonalTokenPost is a request for a new personal token.
type PersonalTokenPost struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	Full          bool     `json:"full"`
	ExpiresInDays int      `json:"expires_in_days"`
}

func signedIn(c *gin.Context) (*oauth.Server, uint, bool) {
	if utils.ViaToken(c) {
		c.JSON(http.StatusForbidden, gin.H{errorKey: "Sign in to manage your API tokens"})
		return nil, 0, false
	}
	srv, ok := server(c)
	if !ok {
		return nil, 0, false
	}
	uid, ok := userID(c)
	return srv, uid, ok
}

// GETPersonalTokens lists the personal API tokens of the logged-in user,
// with the scopes and lifetimes that a new token may have.
func GETPersonalTokens(c *gin.Context) {
	srv, uid, ok := signedIn(c)
	if !ok {
		return
	}
	tokens, err := srv.PersonalTokens(uid)
	if err != nil {
		slog.Error("Failed to list API tokens", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgain})
		return
	}
	now := srv.Now()
	out := make([]PersonalToken, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, personalToken(t, now))
	}
	c.JSON(http.StatusOK, gin.H{"tokens": out, "scopes": oauth.Scopes(), "expiries": oauth.PersonalExpiries()})
}

// POSTPersonalToken makes a personal API token of the logged-in user. The
// response holds its secret, which DMRHub does not show again.
func POSTPersonalToken(c *gin.Context) {
	srv, uid, ok := signedIn(c)
	if !ok {
		return
	}
	var body PersonalTokenPost
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid request body"})
		return
	}
	tok, secret, err := srv.CreatePersonal(uid, oauth.PersonalRequest{
		Name:   body.Name,
		Scopes: strings.Join(body.Scopes, " "),
		Full:   body.Full,
		Days:   body.ExpiresInDays,
	})
	switch {
	case errors.Is(err, oauth.ErrInvalidRequest), errors.Is(err, oauth.ErrInvalidScope):
		c.JSON(http.StatusBadRequest, gin.H{errorKey: personalError(err)})
	case errors.Is(err, oauth.ErrTooManyTokens):
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You have the most tokens you can have. Delete one first."})
	case err != nil:
		slog.Error("Failed to make an API token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgain})
	default:
		answerSecret(c, srv, tok, secret)
	}
}

func personalError(err error) string {
	if errors.Is(err, oauth.ErrInvalidScope) {
		return "Pick at least one thing the token can use, or full account access"
	}
	_, msg, _ := strings.Cut(err.Error(), ": ")
	if msg == "" {
		return "Invalid request"
	}
	return strings.ToUpper(msg[:1]) + msg[1:]
}

func answerSecret(c *gin.Context, srv *oauth.Server, tok models.PersonalToken, secret string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"token": personalToken(tok, srv.Now()), "secret": secret})
}

// DELETEPersonalToken deletes a personal API token of the logged-in user,
// which ends it at once and closes its websockets on every replica.
func DELETEPersonalToken(c *gin.Context) {
	srv, uid, ok := signedIn(c)
	if !ok {
		return
	}
	id := c.Param("id")
	err := srv.RemovePersonal(uid, id)
	switch {
	case errors.Is(err, oauth.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: "Token not found"})
	case err != nil:
		slog.Error("Failed to delete an API token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgain})
	default:
		closeTokenSockets(c, uid, id)
		c.JSON(http.StatusOK, gin.H{"message": "Token deleted"})
	}
}

// POSTRotatePersonalToken replaces a personal API token of the logged-in
// user with a new secret of the same name, access and lifetime. The old
// secret stops working at once and its websockets close on every replica.
// The response holds the new secret, which DMRHub does not show again.
func POSTRotatePersonalToken(c *gin.Context) {
	srv, uid, ok := signedIn(c)
	if !ok {
		return
	}
	id := c.Param("id")
	tok, secret, err := srv.RotatePersonal(uid, id)
	switch {
	case errors.Is(err, oauth.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: "Token not found"})
	case err != nil:
		slog.Error("Failed to rotate an API token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgain})
	default:
		closeTokenSockets(c, uid, id)
		answerSecret(c, srv, tok, secret)
	}
}

func closeTokenSockets(c *gin.Context, userID uint, id string) {
	ps, ok := c.Value("PubSub").(pubsub.PubSub)
	if !ok {
		slog.Error("PubSub not found in context, the websockets of a revoked token stay open", "user", userID)
		return
	}
	if err := websocket.PublishTokenRevoked(ps, userID, models.PersonalGrantPrefix+id); err != nil {
		slog.Error("Failed to close the websockets of a revoked token", "user", userID, "error", err)
	}
}
