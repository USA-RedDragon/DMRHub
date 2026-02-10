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
	"fmt"
	"log/slog"
	"math"
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

func RequireAdminOrTGOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)

		defer func() {
			if recover() != nil {
				slog.Error("Recovered from panic", "function", "RequireLogin")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			slog.Debug("RequireAdminOrTGOwner: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Error("Unable to convert user_id to uint", "function", "RequireAdminOrTGOwner")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			if uid <= math.MaxInt32 {
				span.SetAttributes(
					attribute.String("http.auth", "RequireAdminOrTGOwner"),
					attribute.Int("user.id", int(uid)),
				)
			} else {
				span.SetAttributes(
					attribute.String("http.auth", "RequireAdminOrTGOwner"),
				)
			}
		}

		valid := false
		// Open up the DB and check if the user is an admin
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequireAdminOrTGOwner")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db = db.WithContext(ctx)
		var user models.User
		db.Find(&user, "id = ?", uid)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Admin && user.Approved && !user.Suspended {
			valid = true
		} else {
			// Check if the user is the owner of any talkgroups
			talkgroups, err := models.FindTalkgroupsByOwnerID(db, uid)
			if err != nil {
				slog.Error("Failed to find talkgroups for owner", "function", "RequireAdminOrTGOwner", "userID", uid, "error", err)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
				return
			}
			if len(talkgroups) > 0 && user.Approved && !user.Suspended {
				valid = true
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)

		defer func() {
			if recover() != nil {
				slog.Error("Recovered from panic", "function", "RequireLogin")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			slog.Debug("RequireAdmin: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Error("Unable to convert user_id to uint", "function", "RequireAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			if uid <= math.MaxInt32 {
				span.SetAttributes(
					attribute.String("http.auth", "RequireAdmin"),
					attribute.Int("user.id", int(uid)),
				)
			} else {
				span.SetAttributes(
					attribute.String("http.auth", "RequireAdmin"),
				)
			}
		}

		valid := false
		// Open up the DB and check if the user is an admin
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequireAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db = db.WithContext(ctx)
		var user models.User
		db.Find(&user, "id = ?", uid)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Admin && user.Approved && !user.Suspended {
			valid = true
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireSuperAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		session := sessions.Default(c)

		defer func() {
			if recover() != nil {
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			slog.Debug("RequireSuperAdmin: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Error("Unable to convert user_id to uint", "function", "RequireSuperAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequireSuperAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		user, err := models.FindUserByID(db, uid)
		if err != nil {
			slog.Error("Failed to find user by ID", "function", "RequireSuperAdmin", "userID", uid, "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}

		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			if uid <= math.MaxInt32 {
				span.SetAttributes(
					attribute.String("http.auth", "RequireSuperAdmin"),
					attribute.Int("user.id", int(uid)),
				)
			} else {
				span.SetAttributes(
					attribute.String("http.auth", "RequireSuperAdmin"),
				)
			}
		}
		if !user.SuperAdmin {
			slog.Error("User is not a super admin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)

		defer func() {
			if recover() != nil {
				slog.Error("Recovered from panic", "function", "RequireLogin")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")

		if userID == nil {
			slog.Debug("RequireLogin: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Debug("RequireLogin: Unable to convert user_id to uint")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			if uid <= math.MaxInt32 {
				span.SetAttributes(
					attribute.String("http.auth", "RequireLogin"),
					attribute.Int("user.id", int(uid)),
				)
			} else {
				span.SetAttributes(
					attribute.String("http.auth", "RequireLogin"),
				)
			}
		}

		valid := false
		// Open up the DB and check if the user exists
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequireLogin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db = db.WithContext(ctx)
		var user models.User
		db.Find(&user, "id = ?", uid)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Approved && !user.Suspended {
			valid = true
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequirePeerOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := c.Param("id")
		userID := session.Get("user_id")
		if userID == nil {
			slog.Debug("RequirePeerOwnerOrAdmin: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Error("Unable to convert user_id to uint", "function", "RequirePeerOwnerOrAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			if uid <= math.MaxInt32 {
				span.SetAttributes(
					attribute.String("http.auth", "RequirePeerOwnerOrAdmin"),
					attribute.Int("user.id", int(uid)),
				)
			} else {
				span.SetAttributes(
					attribute.String("http.auth", "RequirePeerOwnerOrAdmin"),
				)
			}
		}

		valid := false
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequirePeerOwnerOrAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db = db.WithContext(ctx)
		// Open up the DB and check if the user is an admin or if they own peer with id = id
		var user models.User
		db.Find(&user, "id = ?", uid)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Approved && !user.Suspended && user.Admin {
			valid = true
		} else {
			var peer models.Peer
			db.Find(&peer, "radio_id = ?", id)
			if peer.OwnerID == user.ID && !user.Suspended && user.Approved {
				valid = true
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireRepeaterOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := c.Param("id")

		defer func() {
			if recover() != nil {
				slog.Error("Recovered from panic", "function", "RequireLogin")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			slog.Debug("RequireRepeaterOwnerOrAdmin: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Error("Unable to convert user_id to uint", "function", "RequireRepeaterOwnerOrAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			if uid <= math.MaxInt32 {
				span.SetAttributes(
					attribute.String("http.auth", "RequireRepeaterOwnerOrAdmin"),
					attribute.Int("user.id", int(uid)),
				)
			} else {
				span.SetAttributes(
					attribute.String("http.auth", "RequireRepeaterOwnerOrAdmin"),
				)
			}
		}

		valid := false
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequireRepeaterOwnerOrAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db = db.WithContext(ctx)
		// Open up the DB and check if the user is an admin or if they own repeater with id = id
		var user models.User
		db.Find(&user, "id = ?", uid)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Approved && !user.Suspended && user.Admin {
			valid = true
		} else {
			var repeater models.Repeater
			db.Find(&repeater, "id = ?", id)
			if repeater.OwnerID == user.ID && !user.Suspended && user.Approved {
				valid = true
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireTalkgroupOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := c.Param("id")

		defer func() {
			if recover() != nil {
				slog.Error("Recovered from panic", "function", "RequireLogin")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			slog.Debug("RequireTalkgroupOwnerOrAdmin: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Error("Unable to convert user_id to uint", "function", "RequireTalkgroupOwnerOrAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			if uid <= math.MaxInt32 {
				span.SetAttributes(
					attribute.String("http.auth", "RequireTalkgroupOwnerOrAdmin"),
					attribute.Int("user.id", int(uid)),
				)
			} else {
				span.SetAttributes(
					attribute.String("http.auth", "RequireTalkgroupOwnerOrAdmin"),
				)
			}
		}

		valid := false
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequireTalkgroupOwnerOrAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db = db.WithContext(ctx)
		// Open up the DB and check if the user is an admin or if they own talkgroup with id = id
		var user models.User
		db.Find(&user, "id = ?", uid)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Admin && !user.Suspended && user.Approved {
			valid = true
		} else {
			var talkgroup models.Talkgroup
			db.Preload("Admins").Find(&talkgroup, "id = ?", id)
			for _, admin := range talkgroup.Admins {
				if admin.ID == user.ID && !user.Suspended && user.Approved {
					valid = true
					break
				}
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireSelfOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := c.Param("id")

		defer func() {
			if recover() != nil {
				slog.Error("Recovered from panic", "function", "RequireLogin")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			slog.Debug("RequireSelfOrAdmin: No user_id found in session")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			slog.Error("Unable to convert user_id to uint", "function", "RequireSelfOrAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			if uid <= math.MaxInt32 {
				span.SetAttributes(
					attribute.String("http.auth", "RequireSelfOrAdmin"),
					attribute.Int("user.id", int(uid)),
				)
			} else {
				span.SetAttributes(
					attribute.String("http.auth", "RequireSelfOrAdmin"),
				)
			}
		}

		valid := false

		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			slog.Error("Unable to get DB from context", "function", "RequireSelfOrAdmin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		db = db.WithContext(ctx)
		// Open up the DB and check if the user is an admin or if their ID matches id
		var user models.User
		db.Find(&user, "id = ?", uid)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Admin && !user.Suspended && user.Approved {
			valid = true
		} else if id == fmt.Sprintf("%d", user.ID) && !user.Suspended && user.Approved {
			valid = true
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}
