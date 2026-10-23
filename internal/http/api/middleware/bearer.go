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

package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// anyScope marks a route that every token may use.
const anyScope = ""

// tokenRoutes are the routes that apps may use with a Bearer token, and the
// scopes that allow each one. Every other route refuses tokens.
func tokenRoutes() map[string][]string {
	return map[string][]string{
		"GET /api/v1/users/me":                  {anyScope},
		"GET /api/v1/network/webtransport":      {anyScope},
		"GET /api/v1/talkgroups":                {oauth.ScopeTalkgroupsRead},
		"GET /api/v1/talkgroups/my":             {oauth.ScopeTalkgroupsRead},
		"GET /api/v1/talkgroups/:id":            {oauth.ScopeTalkgroupsRead},
		"GET /api/v1/lastheard":                 {oauth.ScopeLastHeardRead},
		"GET /api/v1/lastheard/user/:id":        {oauth.ScopeLastHeardRead},
		"GET /api/v1/lastheard/repeater/:id":    {oauth.ScopeLastHeardRead},
		"GET /api/v1/lastheard/talkgroup/:id":   {oauth.ScopeLastHeardRead},
		"GET /api/v1/messages/inbox":            {oauth.ScopeMessagesRead},
		"GET /api/v1/messages/outbox":           {oauth.ScopeMessagesRead},
		"GET /api/v1/messages/stats":            {oauth.ScopeMessagesRead},
		"GET /api/v1/messages/:id":              {oauth.ScopeMessagesRead},
		"GET /ws/messages":                      {oauth.ScopeMessagesRead},
		"GET /api/v1/repeaters/my":              {oauth.ScopeHotspotsRead, oauth.ScopeHotspotsTalkgroups},
		"GET /api/v1/repeaters/:id":             {oauth.ScopeHotspotsRead, oauth.ScopeHotspotsTalkgroups},
		"POST /api/v1/repeaters":                {oauth.ScopeHotspotsRegister},
		"POST /api/v1/repeaters/:id/talkgroups": {oauth.ScopeHotspotsTalkgroups},
	}
}

// Bearer lets apps use the routes of tokenRoutes with an access token from
// srv, as the user who allowed them. The token never carries admin rights,
// and the request never saves a session.
func Bearer(srv *oauth.Server) gin.HandlerFunc {
	routes := tokenRoutes()
	return func(c *gin.Context) {
		access, ok := bearerToken(c)
		if !ok {
			return
		}
		grant, err := srv.Authenticate(access)
		if err != nil {
			if !errors.Is(err, oauth.ErrInvalidToken) {
				slog.Error("Failed to check an access token", "error", err)
			}
			c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: "invalid_token"})
			return
		}
		scopes, known := routes[c.Request.Method+" "+c.FullPath()]
		if !known || !slices.ContainsFunc(scopes, func(s string) bool { return s == anyScope || grant.Allows(s) }) {
			c.Header("WWW-Authenticate", `Bearer error="insufficient_scope"`)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: "insufficient_scope"})
			return
		}
		c.Set(utils.GrantKey, grant)
		sessions.Default(c).Set("user_id", grant.UserID)
	}
}

func bearerToken(c *gin.Context) (string, bool) {
	scheme, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
	if ok && strings.EqualFold(scheme, "Bearer") && token != "" {
		return strings.TrimSpace(token), true
	}
	return websocketToken(c)
}

// WebSocketAccessPrefix starts the subprotocol that carries the access
// token of a websocket, since a browser cannot set the Authorization
// header of one.
const WebSocketAccessPrefix = "dmrhub.bearer."

func websocketToken(c *gin.Context) (string, bool) {
	for _, header := range c.Request.Header.Values("Sec-WebSocket-Protocol") {
		for p := range strings.SplitSeq(header, ",") {
			if token, ok := strings.CutPrefix(strings.TrimSpace(p), WebSocketAccessPrefix); ok && token != "" {
				return token, true
			}
		}
	}
	return "", false
}
