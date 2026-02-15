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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

func SuspendedUserLockout() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				slog.Error("Recovered from panic", "function", "SuspendedUserLockout")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()

		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			slog.Debug("SuspendedUserLockout: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Error("Unable to convert user_id to uint", "function", "SuspendedUserLockout")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}

		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "SuspendedUserLockout")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db = db.WithContext(c.Request.Context())

		user, err := models.FindUserByID(db, uid)
		if err != nil {
			slog.Error("Unable to find user by ID", "function", "SuspendedUserLockout")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}

		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.suspended", user.Suspended),
			)
		}

		if user.Suspended {
			slog.Error("User is suspended", "function", "SuspendedUserLockout")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User is suspended"})
			return
		}
	}
}
