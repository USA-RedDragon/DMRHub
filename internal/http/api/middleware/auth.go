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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

const (
	errorKey      = "error"
	msgAuthFailed = "Authentication failed"
)

// MsgNotRepeaterOwner is the error of a request about a repeater of
// another user.
const MsgNotRepeaterOwner = "You do not own this repeater"

// authenticateUser performs the common session extraction, panic recovery,
// tracing, and DB user lookup shared by all Require* middleware functions.
// It returns the authenticated user, the contextualized DB handle, and true
// on success. On failure it aborts the request and returns false.
func authenticateUser(c *gin.Context, authName string) (models.User, *gorm.DB, bool) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		slog.Debug(authName+": No user_id found in session", "function", authName)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
		return models.User{}, nil, false
	}
	uid, ok := userID.(uint)
	if !ok {
		slog.Error("Unable to convert user_id to uint", "function", authName)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
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
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
		return models.User{}, nil, false
	}
	db = db.WithContext(ctx)

	var user models.User
	db.Find(&user, "id = ?", uid)
	if utils.ViaToken(c) {
		user.Admin = false
		user.SuperAdmin = false
	}
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
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
	}
}

func RequireAdminOrTGOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		user, db, ok := authenticateUser(c, "RequireAdminOrTGOwner")
		if !ok {
			return
		}
		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}
		if user.Admin {
			return
		}

		talkgroups, err := models.FindTalkgroupsByOwnerID(db, user.ID)
		if err != nil {
			slog.Error("Failed to find talkgroups for owner", "function", "RequireAdminOrTGOwner", "userID", user.ID, "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}
		if len(talkgroups) > 0 {
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotAdminOrTalkgroupOwner})
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer panicRecovery(c)

		user, _, ok := authenticateUser(c, "RequireAdmin")
		if !ok {
			return
		}
		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}
		if !user.Admin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotAdmin})
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

		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}
		if !user.SuperAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotSuperAdmin})
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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
		}
	}
}

// RequireLoginToView is RequireLogin while visibility is login, and lets
// every request through otherwise. It reads the config at each request.
func RequireLoginToView(current config.Source) gin.HandlerFunc {
	login := RequireLogin()
	return func(c *gin.Context) {
		if current.Get().Visibility.Mode() == config.VisibilityLogin {
			login(c)
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
		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}
		if user.Admin {
			return
		}

		var peer models.Peer
		db.Find(&peer, "id = ?", id)
		if peer.OwnerID == user.ID {
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotPeerOwner})
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

		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}
		if user.Admin {
			return
		}

		var repeater models.Repeater
		db.Find(&repeater, "id = ?", id)
		if repeater.OwnerID == user.ID {
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotRepeaterOwner})
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
		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}
		if user.Admin {
			return
		}

		var talkgroup models.Talkgroup
		db.Preload("Admins").Find(&talkgroup, "id = ?", id)
		for _, admin := range talkgroup.Admins {
			if admin.ID == user.ID {
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotTalkgroupAdmin})
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
		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}
		if user.Admin || id == fmt.Sprintf("%d", user.ID) {
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotSelf})
	}
}

// isTalkgroupNCOOrOwnerOrAdmin checks if the given approved, unsuspended
// user is a system admin, a talkgroup admin, or a talkgroup NCO for the
// talkgroup identified by talkgroupID.
// CanRunNets reports whether user may start, stop and schedule the nets of
// the talkgroup talkgroupID: an admin, an admin of the talkgroup, or one of
// its NCOs.
func CanRunNets(db *gorm.DB, user models.User, talkgroupID uint) bool {
	return isTalkgroupNCOOrOwnerOrAdmin(db, user, talkgroupID)
}

func isTalkgroupNCOOrOwnerOrAdmin(db *gorm.DB, user models.User, talkgroupID interface{}) bool {
	if user.Admin {
		return true
	}

	var talkgroup models.Talkgroup
	db.Preload("Admins").Preload("NCOs").Find(&talkgroup, "id = ?", talkgroupID)
	for _, admin := range talkgroup.Admins {
		if admin.ID == user.ID {
			return true
		}
	}
	for _, nco := range talkgroup.NCOs {
		if nco.ID == user.ID {
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
		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}

		if isTalkgroupNCOOrOwnerOrAdmin(db, user, tgID) {
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotTalkgroupNCO})
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
		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}

		var net models.Net
		if err := db.First(&net, "id = ?", id).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{errorKey: "Net not found"})
			return
		}

		if isTalkgroupNCOOrOwnerOrAdmin(db, user, net.TalkgroupID) {
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotTalkgroupNCO})
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
		if !user.Approved || user.Suspended {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
			return
		}

		var sn models.ScheduledNet
		if err := db.First(&sn, "id = ?", id).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{errorKey: "Scheduled net not found"})
			return
		}

		if isTalkgroupNCOOrOwnerOrAdmin(db, user, sn.TalkgroupID) {
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{errorKey: MsgNotTalkgroupNCO})
	}
}
