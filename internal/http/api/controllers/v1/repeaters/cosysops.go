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

package repeaters

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	roleOwner   = "owner"
	roleCosysop = "cosysop"
	roleAdmin   = "admin"

	msgErrCosysops     = "Error getting the co-sysops"
	msgNoInvitee       = "No user has that callsign or username"
	msgCosysopIsOwner  = "That user owns this repeater"
	msgCosysopExists   = "That user is a co-sysop already or has an invite"
	msgNoInvite        = "There is no open invite to this repeater"
	msgNotACosysop     = "That user is not a co-sysop of this repeater"
	msgOnlyOwnerRemove = "Only the owner removes other co-sysops"
)

func sessionUser(c *gin.Context, db *gorm.DB) (models.User, bool) {
	uid, ok := sessions.Default(c).Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
		return models.User{}, false
	}
	user, err := models.FindUserByID(db, uid)
	if err != nil {
		slog.Error("Error finding the user of a request", "userID", uid, "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
		return models.User{}, false
	}
	if utils.ViaToken(c) {
		user.Admin = false
	}
	return user, true
}

func cosysopRepeater(c *gin.Context, db *gorm.DB) (models.Repeater, bool) {
	id, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return models.Repeater{}, false
	}
	var rpt models.Repeater
	err = db.Preload("Owner").First(&rpt, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoRepeater})
		return models.Repeater{}, false
	}
	if err != nil {
		slog.Error("Error finding a repeater", "repeaterID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeater})
		return models.Repeater{}, false
	}
	return rpt, true
}

func ownerRef(rpt *models.Repeater) *models.UserRef {
	if rpt.OwnerID == 0 {
		return nil
	}
	ref := models.NewUserRef(&rpt.Owner)
	return &ref
}

// GETCosysops lists the owner and the co-sysops of a repeater. The owner
// and admins also get the invites that wait.
func GETCosysops(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := sessionUser(c, db)
	if !ok {
		return
	}
	rpt, ok := cosysopRepeater(c, db)
	if !ok {
		return
	}
	role := roleCosysop
	switch {
	case rpt.OwnerID == user.ID:
		role = roleOwner
	case user.Admin:
		role = roleAdmin
	}
	now := time.Now()
	entries, err := models.ListCosysops(db, rpt.ID, role != roleCosysop, now)
	if err != nil {
		slog.Error(msgErrCosysops, "repeaterID", rpt.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrCosysops})
		return
	}
	out := apimodels.Cosysops{Owner: ownerRef(&rpt), Role: role, Cosysops: make([]apimodels.Cosysop, 0, len(entries))}
	for _, e := range entries {
		item := apimodels.Cosysop{ID: e.UserID, Callsign: e.Callsign, Username: e.Username, InvitedAt: e.InvitedAt, AcceptedAt: e.AcceptedAt, Pending: e.AcceptedAt == nil}
		if item.Pending {
			exp := e.ExpiresAt
			item.ExpiresAt = &exp
		}
		out.Cosysops = append(out.Cosysops, item)
	}
	c.JSON(http.StatusOK, out)
}

// POSTCosysop invites the user of the callsign or username in the body to
// be a co-sysop of the repeater. The invite waits for
// models.CosysopInviteLifetime.
func POSTCosysop(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := sessionUser(c, db)
	if !ok {
		return
	}
	rpt, ok := cosysopRepeater(c, db)
	if !ok {
		return
	}
	var post apimodels.CosysopPost
	if err := c.ShouldBindJSON(&post); err != nil || strings.TrimSpace(post.User) == "" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	name := strings.TrimSpace(post.User)
	invitee, found, err := models.FindUserByCallsign(db, name)
	if err == nil && !found {
		invitee, found, err = models.FindUserByUsername(db, name)
	}
	if err != nil {
		slog.Error("Error finding a user to invite", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	if !found || !invitee.Approved || invitee.Suspended {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgNoInvitee})
		return
	}
	inv, err := models.InviteCosysop(db, rpt.ID, invitee.ID, user.ID, time.Now())
	switch {
	case errors.Is(err, models.ErrCosysopIsOwner):
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgCosysopIsOwner})
		return
	case errors.Is(err, models.ErrCosysopExists):
		c.JSON(http.StatusConflict, gin.H{errorKey: msgCosysopExists})
		return
	case err != nil:
		slog.Error("Error inviting a co-sysop", "repeaterID", rpt.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	exp := inv.ExpiresAt
	c.JSON(http.StatusCreated, apimodels.Cosysop{ID: invitee.ID, Callsign: invitee.Callsign, Username: invitee.Username, InvitedAt: inv.InvitedAt, ExpiresAt: &exp, Pending: true})
}

// DELETECosysop removes a co-sysop of the repeater or takes back an
// invite. The owner and admins remove anyone; a co-sysop removes only
// themselves.
func DELETECosysop(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := sessionUser(c, db)
	if !ok {
		return
	}
	rpt, ok := cosysopRepeater(c, db)
	if !ok {
		return
	}
	target, err := strconv.ParseUint(c.Param("user"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid user ID"})
		return
	}
	if rpt.OwnerID != user.ID && !user.Admin && uint(target) != user.ID {
		c.JSON(http.StatusForbidden, gin.H{errorKey: msgOnlyOwnerRemove})
		return
	}
	removed, err := models.RemoveCosysop(db, rpt.ID, uint(target))
	if err != nil {
		slog.Error("Error removing a co-sysop", "repeaterID", rpt.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	if !removed {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotACosysop})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "Co-sysop removed"})
}

// GETCosysopInvites lists the invites of the user to be a co-sysop that
// wait.
func GETCosysopInvites(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := sessionUser(c, db)
	if !ok {
		return
	}
	invites, err := models.PendingCosysopInvites(db, user.ID, time.Now())
	if err != nil {
		slog.Error("Error listing co-sysop invites", "userID", user.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	out := make([]apimodels.CosysopInvite, 0, len(invites))
	for _, inv := range invites {
		var rpt models.Repeater
		if err := db.Preload("Owner").First(&rpt, inv.RepeaterID).Error; err != nil {
			continue
		}
		out = append(out, apimodels.CosysopInvite{RepeaterID: rpt.ID, RepeaterCallsign: rpt.Callsign, Owner: ownerRef(&rpt), InvitedAt: inv.InvitedAt, ExpiresAt: inv.ExpiresAt})
	}
	c.JSON(http.StatusOK, gin.H{"invites": out})
}

// POSTCosysopAccept accepts the invite of the user to be a co-sysop of the
// repeater.
func POSTCosysopAccept(c *gin.Context) {
	answerCosysopInvite(c, true)
}

// POSTCosysopDecline declines the invite of the user to be a co-sysop of
// the repeater.
func POSTCosysopDecline(c *gin.Context) {
	answerCosysopInvite(c, false)
}

func answerCosysopInvite(c *gin.Context, accept bool) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := sessionUser(c, db)
	if !ok {
		return
	}
	id, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}
	done, err := models.AnswerCosysopInvite(db, id, user.ID, accept, time.Now())
	if err != nil {
		slog.Error("Error answering a co-sysop invite", "repeaterID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	if !done {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoInvite})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "Done"})
}
