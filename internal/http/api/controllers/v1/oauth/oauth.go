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

// Package oauth answers the OAuth requests of web apps and the requests of
// the consent page and the list of apps.
package oauth

import (
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// ServerKey is the key of the *oauth.Server in the gin context.
const ServerKey = "OAuth"

const (
	errorKey          = "error"
	descriptionKey    = "error_description"
	errServerError    = "server_error"
	errInvalidRequest = "invalid_request"
	responseTypeCode  = "code"
	grantCode         = "authorization_code"
	grantRefresh      = "refresh_token"
)

func server(c *gin.Context) (*oauth.Server, bool) {
	srv, ok := c.MustGet(ServerKey).(*oauth.Server)
	if !ok {
		slog.Error("OAuth server not found in context")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: errServerError})
	}
	return srv, ok
}

// Issuer returns the URL that DMRHub is reached at, without a trailing
// slash.
func Issuer(c *gin.Context) string {
	if cfg, ok := c.Value("Config").(*config.Config); ok && cfg.HTTP.CanonicalHost != "" {
		return strings.TrimSuffix(cfg.HTTP.CanonicalHost, "/")
	}
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

// GETMetadata answers the discovery document of RFC 8414.
func GETMetadata(c *gin.Context) {
	issuer := Issuer(c)
	scopes := oauth.Scopes()
	names := make([]string, 0, len(scopes))
	for _, s := range scopes {
		names = append(names, s.Name)
	}
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                         issuer,
		"authorization_endpoint":                         issuer + "/oauth/authorize",
		"token_endpoint":                                 issuer + "/api/v1/oauth/token",
		"revocation_endpoint":                            issuer + "/api/v1/oauth/revoke",
		"response_types_supported":                       []string{responseTypeCode},
		"grant_types_supported":                          []string{grantCode, grantRefresh},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"scopes_supported":                               names,
		"authorization_response_iss_parameter_supported": true,
	})
}

type authorizeParams struct {
	ResponseType        string `json:"response_type" form:"response_type"`
	ClientID            string `json:"client_id" form:"client_id"`
	RedirectURI         string `json:"redirect_uri" form:"redirect_uri"`
	Scope               string `json:"scope" form:"scope"`
	State               string `json:"state" form:"state"`
	CodeChallenge       string `json:"code_challenge" form:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method" form:"code_challenge_method"`
	Approve             bool   `json:"approve"`
}

// check checks p. When the app may be told about a failure, the returned
// redirect sends the error to it.
func (p authorizeParams) check(c *gin.Context, srv *oauth.Server) (oauth.AuthorizeRequest, string, error) {
	req, err := srv.ParseAuthorize(p.ClientID, p.RedirectURI, p.Scope, p.CodeChallenge, p.CodeChallengeMethod)
	if errors.Is(err, oauth.ErrInvalidClient) {
		return req, "", fmt.Errorf("authorize: %w", err)
	}
	if err == nil && p.ResponseType != responseTypeCode {
		err = errUnsupportedResponseType
	}
	if err != nil {
		return req, p.redirect(c, url.Values{errorKey: {errorCode(err)}}), err
	}
	return req, "", nil
}

var errUnsupportedResponseType = errors.New("unsupported_response_type")

func (p authorizeParams) redirect(c *gin.Context, v url.Values) string {
	if p.State != "" {
		v.Set("state", p.State)
	}
	v.Set("iss", Issuer(c))
	sep := "?"
	if strings.Contains(p.RedirectURI, "?") {
		sep = "&"
	}
	return p.RedirectURI + sep + v.Encode()
}

func errorCode(err error) string {
	for _, e := range []error{oauth.ErrInvalidRequest, oauth.ErrInvalidClient, oauth.ErrInvalidScope, oauth.ErrInvalidGrant, errUnsupportedResponseType} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return errServerError
}

func badAuthorize(c *gin.Context, redirect string, err error) {
	body := gin.H{errorKey: errorCode(err), descriptionKey: err.Error()}
	if redirect != "" {
		body["redirect"] = redirect
	}
	c.JSON(http.StatusBadRequest, body)
}

// GETAuthorize checks an authorization request and answers what the
// consent page shows: the origin of the app and what it asks for.
func GETAuthorize(c *gin.Context) {
	srv, ok := server(c)
	if !ok {
		return
	}
	var p authorizeParams
	if err := c.ShouldBindQuery(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: errInvalidRequest})
		return
	}
	req, redirect, err := p.check(c, srv)
	if err != nil {
		badAuthorize(c, redirect, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"origin": req.ClientID, "scopes": req.Scopes})
}

// POSTAuthorize records the answer of the logged-in user on the consent
// page and answers where to send the browser. Only the page of DMRHub
// itself may send it.
func POSTAuthorize(c *gin.Context) {
	if ct, _, _ := mime.ParseMediaType(c.ContentType()); ct != "application/json" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{errorKey: errInvalidRequest})
		return
	}
	if !sameOrigin(c) {
		c.JSON(http.StatusForbidden, gin.H{errorKey: "access_denied"})
		return
	}
	srv, ok := server(c)
	if !ok {
		return
	}
	userID, ok := sessions.Default(c).Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: "Authentication failed"})
		return
	}
	var p authorizeParams
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: errInvalidRequest})
		return
	}
	req, redirect, err := p.check(c, srv)
	if err != nil {
		badAuthorize(c, redirect, err)
		return
	}
	if !p.Approve {
		c.JSON(http.StatusOK, gin.H{"redirect": p.redirect(c, url.Values{errorKey: {"access_denied"}})})
		return
	}
	code, err := srv.Approve(userID, req)
	if err != nil {
		slog.Error("Failed to approve an app", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: errServerError})
		return
	}
	c.JSON(http.StatusOK, gin.H{"redirect": p.redirect(c, url.Values{"code": {code}})})
}

func sameOrigin(c *gin.Context) bool {
	if site := c.GetHeader("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	u, err := url.Parse(c.GetHeader("Origin"))
	return err == nil && u.Host != "" && u.Host == c.Request.Host
}

// POSTToken exchanges a code or a refresh token for tokens.
func POSTToken(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	srv, ok := server(c)
	if !ok {
		return
	}
	clientID := c.PostForm("client_id")
	var tokens oauth.Tokens
	var err error
	switch c.PostForm("grant_type") {
	case grantCode:
		tokens, err = srv.Exchange(c.PostForm("code"), clientID, c.PostForm("redirect_uri"), c.PostForm("code_verifier"))
	case grantRefresh:
		tokens, err = srv.Refresh(c.PostForm("refresh_token"), clientID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "unsupported_grant_type"})
		return
	}
	if err != nil {
		code := errorCode(err)
		if code == errServerError {
			slog.Error("Failed to issue tokens", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: code})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{errorKey: code})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token": tokens.Access,
		"token_type":   "Bearer",
		"expires_in":   int(tokens.ExpiresIn / time.Second),
		grantRefresh:   tokens.Refresh,
		"scope":        tokens.Scope,
	})
}

// POSTRevoke ends the grant of a token of the app, as RFC 7009 asks; an
// app revokes its refresh token when its user logs out.
func POSTRevoke(c *gin.Context) {
	srv, ok := server(c)
	if !ok {
		return
	}
	grant, revoked, err := srv.Revoke(c.PostForm("token"), c.PostForm("client_id"))
	if err != nil {
		slog.Error("Failed to revoke a token", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{errorKey: "temporarily_unavailable"})
		return
	}
	if revoked {
		closeAppSockets(c, grant.UserID, grant.ID)
	}
	c.Status(http.StatusOK)
}

func closeAppSockets(c *gin.Context, userID uint, grantID string) {
	ps, ok := c.Value("PubSub").(pubsub.PubSub)
	if !ok {
		slog.Error("PubSub not found in context, the websockets of a removed app stay open", "user", userID)
		return
	}
	if err := websocket.PublishAppRemoved(ps, userID, grantID); err != nil {
		slog.Error("Failed to close the websockets of a removed app", "user", userID, "error", err)
	}
}

// App is an app that the user allowed, as the Settings page lists it.
type App struct {
	ID       string    `json:"id"`
	Origin   string    `json:"origin"`
	CanUse   string    `json:"can_use"`
	Allowed  time.Time `json:"allowed"`
	LastUsed time.Time `json:"last_used"`
}

func userID(c *gin.Context) (uint, bool) {
	id, ok := sessions.Default(c).Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: "Authentication failed"})
	}
	return id, ok
}

// GETApps lists the apps that the logged-in user allowed.
func GETApps(c *gin.Context) {
	srv, ok := server(c)
	if !ok {
		return
	}
	uid, ok := userID(c)
	if !ok {
		return
	}
	grants, err := srv.Grants(uid)
	if err != nil {
		slog.Error("Failed to list apps", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return
	}
	apps := make([]App, 0, len(grants))
	for _, g := range grants {
		apps = append(apps, App{ID: g.ID, Origin: g.ClientID, CanUse: oauth.Summary(g.Scopes), Allowed: g.CreatedAt, LastUsed: g.LastUsedAt})
	}
	c.JSON(http.StatusOK, apps)
}

// DELETEApp removes an app that the logged-in user allowed, which ends its
// access at once.
func DELETEApp(c *gin.Context) {
	srv, ok := server(c)
	if !ok {
		return
	}
	uid, ok := userID(c)
	if !ok {
		return
	}
	err := srv.RemoveGrant(uid, c.Param("id"))
	switch {
	case errors.Is(err, oauth.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: "App not found"})
	case err != nil:
		slog.Error("Failed to remove an app", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
	default:
		closeAppSockets(c, uid, c.Param("id"))
		c.JSON(http.StatusOK, gin.H{"message": "App removed"})
	}
}
