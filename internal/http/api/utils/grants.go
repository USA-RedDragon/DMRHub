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
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/gin-gonic/gin"
)

// OAuthServerKey is the key of the *oauth.Server in the gin context, the
// same as the ServerKey of the OAuth controllers.
const OAuthServerKey = "OAuth"

// RevokeUserGrants ends every app grant of the user userID. It reports
// false when they could not be ended.
func RevokeUserGrants(c *gin.Context, userID uint) bool {
	srv, ok := c.Value(OAuthServerKey).(*oauth.Server)
	if !ok {
		slog.Error("OAuth server not found in context, the apps of a user keep their access", "user", userID)
		return false
	}
	if err := srv.RevokeUserGrants(userID); err != nil {
		slog.Error("Failed to end the apps of a user", "user", userID, "error", err)
		return false
	}
	return true
}
