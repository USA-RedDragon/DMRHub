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

// authenticateUser performs the common session extraction, panic recovery,
// tracing, and DB user lookup shared by all Require* middleware functions.
// It returns the authenticated user, the contextualized DB handle, and true
// on success. On failure it aborts the request and returns false.
func authenticateUser(c *gin.Context, authName string) (models.User, *gorm.DB, bool) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		slog.Debug(authName+": No user_id found in session", "function", authName)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return models.User{}, nil, false
	}
	uid, ok := userID.(uint)
	if !ok {
		slog.Error("Unable to convert user_id to uint", "function", authName)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return models.User{}, nil, false
	}

	ctx := c.Request.Context()
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		attrs := []attribute.KeyValue{
			attribute.String("http.auth", authName),
		}
		if uid <= math.MaxInt32 {
			attrs = append(attrs, attribute.Int("user.id", int(uid)))
		}
		span.SetAttributes(attrs...)
	}

	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context", "function", authName)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return models.User{}, nil, false
	}
	db = db.WithContext(ctx)

	var user models.User
	db.Find(&user, "id = ?", uid)
	if span.IsRecording() {
		span.SetAttributes(attribute.Bool("user.admin", user.Admin))
	}

	return user, db, true
}

// panicRecovery returns a deferred function suitable for recovering from
// session-related panics in auth middleware.
func panicRecovery(c *gin.Context) {
	if recover() != nil {
		slog.Error("Recovered from panic in auth middleware")
		c.SetCookie("sessions", "", -1, "/", "", false, true)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}

func RequireAdminOrTGOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		user, db, ok := authenticateUser(c, "RequireAdminOrTGOwner")
		if !ok {
			return
		}

		if user.Admin && user.Approved && !user.Suspended {
			return
		}

		talkgroups, err := models.FindTalkgroupsByOwnerID(db, user.ID)
		if err != nil {
			slog.Error("Failed to find talkgroups for owner", "function", "RequireAdminOrTGOwner", "userID", user.ID, "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		if len(talkgroups) > 0 && user.Approved && !user.Suspended {
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		user, _, ok := authenticateUser(c, "RequireAdmin")
		if !ok {
			return
		}

		if !user.Admin || !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireSuperAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		user, _, ok := authenticateUser(c, "RequireSuperAdmin")
		if !ok {
			return
		}

		if !user.SuperAdmin || !user.Approved || user.Suspended {
			slog.Error("User is not a super admin or is not approved/suspended")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		user, _, ok := authenticateUser(c, "RequireLogin")
		if !ok {
			return
		}

		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequirePeerOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		id := c.Param("id")
		user, db, ok := authenticateUser(c, "RequirePeerOwnerOrAdmin")
		if !ok {
			return
		}

		if user.Approved && !user.Suspended && user.Admin {
			return
		}

		var peer models.Peer
		db.Find(&peer, "radio_id = ?", id)
		if peer.OwnerID == user.ID && !user.Suspended && user.Approved {
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}

func RequireRepeaterOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		id := c.Param("id")
		user, db, ok := authenticateUser(c, "RequireRepeaterOwnerOrAdmin")
		if !ok {
			return
		}

		if user.Approved && !user.Suspended && user.Admin {
			return
		}

		var repeater models.Repeater
		db.Find(&repeater, "id = ?", id)
		if repeater.OwnerID == user.ID && !user.Suspended && user.Approved {
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}

func RequireTalkgroupOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		id := c.Param("id")
		user, db, ok := authenticateUser(c, "RequireTalkgroupOwnerOrAdmin")
		if !ok {
			return
		}

		if user.Admin && !user.Suspended && user.Approved {
			return
		}

		var talkgroup models.Talkgroup
		db.Preload("Admins").Find(&talkgroup, "id = ?", id)
		for _, admin := range talkgroup.Admins {
			if admin.ID == user.ID && !user.Suspended && user.Approved {
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}

func RequireSelfOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		id := c.Param("id")
		user, _, ok := authenticateUser(c, "RequireSelfOrAdmin")
		if !ok {
			return
		}

		if user.Admin && !user.Suspended && user.Approved {
			return
		}
		if id == fmt.Sprintf("%d", user.ID) && !user.Suspended && user.Approved {
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}

// isTalkgroupNCOOrOwnerOrAdmin checks if the given user is a system admin,
// a talkgroup admin, or a talkgroup NCO for the talkgroup identified by talkgroupID.
func isTalkgroupNCOOrOwnerOrAdmin(db *gorm.DB, user models.User, talkgroupID interface{}) bool {
	if user.Admin && !user.Suspended && user.Approved {
		return true
	}

	var talkgroup models.Talkgroup
	db.Preload("Admins").Preload("NCOs").Find(&talkgroup, "id = ?", talkgroupID)
	for _, admin := range talkgroup.Admins {
		if admin.ID == user.ID && !user.Suspended && user.Approved {
			return true
		}
	}
	for _, nco := range talkgroup.NCOs {
		if nco.ID == user.ID && !user.Suspended && user.Approved {
			return true
		}
	}
	return false
}

// RequireTalkgroupNCOOrOwnerOrAdmin authorises system admins, talkgroup admins,
// and talkgroup NCOs. Expects a :talkgroup_id route parameter.
func RequireTalkgroupNCOOrOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		tgID := c.Param("talkgroup_id")
		user, db, ok := authenticateUser(c, "RequireTalkgroupNCOOrOwnerOrAdmin")
		if !ok {
			return
		}

		if isTalkgroupNCOOrOwnerOrAdmin(db, user, tgID) {
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}

// RequireNetNCOOrOwnerOrAdmin authorises system admins, the net's talkgroup
// admins, and the net's talkgroup NCOs. Resolves the talkgroup from a Net
// identified by the :id route parameter.
func RequireNetNCOOrOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		id := c.Param("id")
		user, db, ok := authenticateUser(c, "RequireNetNCOOrOwnerOrAdmin")
		if !ok {
			return
		}

		var net models.Net
		if err := db.First(&net, "id = ?", id).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Net not found"})
			return
		}

		if isTalkgroupNCOOrOwnerOrAdmin(db, user, net.TalkgroupID) {
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}

// RequireScheduledNetNCOOrOwnerOrAdmin authorises system admins, the scheduled
// net's talkgroup admins, and its talkgroup NCOs. Resolves the talkgroup from a
// ScheduledNet identified by the :id route parameter.
func RequireScheduledNetNCOOrOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		id := c.Param("id")
		user, db, ok := authenticateUser(c, "RequireScheduledNetNCOOrOwnerOrAdmin")
		if !ok {
			return
		}

		var sn models.ScheduledNet
		if err := db.First(&sn, "id = ?", id).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Scheduled net not found"})
			return
		}

		if isTalkgroupNCOOrOwnerOrAdmin(db, user, sn.TalkgroupID) {
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
}
