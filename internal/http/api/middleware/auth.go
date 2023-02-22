// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func RequireAdminOrTGOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)

		defer func() {
			if recover() != nil {
				klog.Error("RequireLogin: Recovered from panic")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			if config.GetConfig().Debug {
				klog.Error("RequireAdminOrTGOwner: Failed to get user_id from session")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			klog.Error("RequireAdminOrTGOwner: Unable to convert user_id to uint")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireAdminOrTGOwner"),
				attribute.Int("user.id", int(uid)),
			)
		}

		valid := false
		// Open up the DB and check if the user is an admin
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			klog.Error("RequireAdminOrTGOwner: Unable to get DB from context")
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

		// Check if the user is the owner of any talkgroups
		talkgroups, err := models.FindTalkgroupsByOwnerID(db, uid)
		if err != nil {
			klog.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		if len(talkgroups) > 0 && user.Approved && !user.Suspended {
			valid = true
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
				klog.Error("RequireLogin: Recovered from panic")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			if config.GetConfig().Debug {
				klog.Error("RequireAdmin: Failed to get user_id from session")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			klog.Error("RequireAdmin: Unable to convert user_id to uint")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireAdmin"),
				attribute.Int("user.id", int(uid)),
			)
		}

		valid := false
		// Open up the DB and check if the user is an admin
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			klog.Error("RequireAdmin: Unable to get DB from context")
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
			if config.GetConfig().Debug {
				klog.Error("RequireSuperAdmin: Failed to get user_id from session")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			klog.Error("RequireSuperAdmin: Unable to convert user_id to uint")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireAdmin"),
				attribute.Int("user.id", int(uid)),
			)
		}
		if uid != dmrconst.SuperAdminUser {
			klog.Error("User is not a super admin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)

		defer func() {
			if recover() != nil {
				klog.Error("RequireLogin: Recovered from panic")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")

		if userID == nil {
			if config.GetConfig().Debug {
				klog.Error("RequireLogin: Failed to get user_id from session")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			klog.Error("RequireLogin: Unable to convert user_id to uint")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireLogin"),
				attribute.Int("user.id", int(uid)),
			)
		}

		valid := false
		// Open up the DB and check if the user exists
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			klog.Error("RequireLogin: Unable to get DB from context")
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

func RequireRepeaterOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := c.Param("id")

		defer func() {
			if recover() != nil {
				klog.Error("RequireLogin: Recovered from panic")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			if config.GetConfig().Debug {
				klog.Error("RequireRepeaterOwnerOrAdmin: Failed to get user_id from session")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			klog.Error("RequireRepeaterOwnerOrAdmin: Unable to convert user_id to uint")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireRepeaterOwnerOrAdmin"),
				attribute.Int("user.id", int(uid)),
			)
		}

		valid := false
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			klog.Error("RequireRepeaterOwnerOrAdmin: Unable to get DB from context")
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
			db.Find(&repeater, "radio_id = ?", id)
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
				klog.Error("RequireLogin: Recovered from panic")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			if config.GetConfig().Debug {
				klog.Error("RequireTalkgroupOwnerOrAdmin: Failed to get user_id from session")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			klog.Error("RequireTalkgroupOwnerOrAdmin: Unable to convert user_id to uint")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireTalkgroupOwnerOrAdmin"),
				attribute.Int("user.id", int(uid)),
			)
		}

		valid := false
		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			klog.Error("RequireTalkgroupOwnerOrAdmin: Unable to get DB from context")
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
				klog.Error("RequireLogin: Recovered from panic")
				// Delete the session cookie
				c.SetCookie("sessions", "", -1, "/", "", false, true)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			}
		}()
		userID := session.Get("user_id")
		if userID == nil {
			if config.GetConfig().Debug {
				klog.Error("RequireSelfOrAdmin: Failed to get user_id from session")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		uid, ok := userID.(uint)
		if !ok {
			klog.Error("RequireSelfOrAdmin: Unable to convert user_id to uint")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireSelfOrAdmin"),
				attribute.Int("user.id", int(uid)),
			)
		}

		valid := false

		db, ok := c.MustGet("DB").(*gorm.DB)
		if !ok {
			klog.Error("RequireSelfOrAdmin: Unable to get DB from context")
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
