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
	"log/slog"
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// MsgMustChangePassword is the error of a request from a user whose
// password an admin set, to a route that RequireOwnPassword closes.
const MsgMustChangePassword = "An admin set a temporary password for you. Choose your own password in Settings first." //nolint:gosec // an error text, not a credential

// ownPasswordRoutes are the routes that stay open to a user whose
// password an admin set: logging in and out, a forgotten password,
// reading their own account, and the facts about the network that every
// page shows.
func ownPasswordRoutes() map[string]bool {
	return map[string]bool{
		"POST /api/v1/auth/login":                true,
		"POST /api/v1/auth/logout":               true,
		"POST /api/v1/auth/forgot-password":      true,
		"POST /api/v1/auth/reset-password":       true,
		"POST /api/v1/auth/reset-password/check": true,
		"GET /api/v1/users/me":                   true,
		"GET /api/v1/network/name":               true,
		"GET /api/v1/network/features":           true,
		"GET /api/v1/version":                    true,
		"GET /api/v1/ping":                       true,
		"GET /api/v1/healthcheck":                true,
	}
}

// RequireOwnPassword answers 403 to every request of a user whose password
// an admin set, until the user chooses their own with PATCH
// /api/v1/users/:id. The routes of ownPasswordRoutes stay open. Requests
// without a logged-in user pass.
func RequireOwnPassword() gin.HandlerFunc {
	open := ownPasswordRoutes()
	return func(c *gin.Context) {
		if _, ok := c.Get(sessions.DefaultKey); !ok {
			return
		}
		uid, ok := sessions.Default(c).Get("user_id").(uint)
		if !ok {
			return
		}
		db, ok := c.Value("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequireOwnPassword")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
			return
		}
		var marks []bool
		if err := db.Model(&models.User{}).Where("id = ?", uid).Pluck("must_change_password", &marks).Error; err != nil {
			slog.Error("Unable to read whether a password must change", "function", "RequireOwnPassword", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
			return
		}
		if len(marks) == 0 || !marks[0] {
			return
		}
		route := c.Request.Method + " " + c.FullPath()
		if open[route] {
			return
		}
		if route == "PATCH /api/v1/users/:id" && c.Param("id") == strconv.FormatUint(uint64(uid), 10) {
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgMustChangePassword, "must_change_password": true})
	}
}
