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
package utils

import (
	"context"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-gonic/gin"
)

// GrantKey is the key of the OAuth grant of a request that an app made
// with a Bearer token.
const GrantKey = "OAuthGrant"

// Grant returns the OAuth grant of a request that an app made with a
// Bearer token.
func Grant(c *gin.Context) (models.OAuthGrant, bool) {
	v, ok := c.Get(GrantKey)
	if !ok {
		return models.OAuthGrant{}, false
	}
	g, ok := v.(models.OAuthGrant)
	return g, ok
}

// ViaToken reports whether an app made the request with a Bearer token.
// Such a request acts as its user but never with admin rights.
func ViaToken(c *gin.Context) bool {
	_, ok := Grant(c)
	return ok
}

type grantContextKey struct{}

// ContextWithGrant returns ctx carrying g, the OAuth grant of the request
// that ctx belongs to.
func ContextWithGrant(ctx context.Context, g models.OAuthGrant) context.Context {
	return context.WithValue(ctx, grantContextKey{}, g)
}

// GrantFromContext returns the OAuth grant that ContextWithGrant put in
// ctx, and false for a request made without a Bearer token.
func GrantFromContext(ctx context.Context) (models.OAuthGrant, bool) {
	g, ok := ctx.Value(grantContextKey{}).(models.OAuthGrant)
	return g, ok
}
