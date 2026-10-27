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

package users

import (
	"fmt"
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

// GETInvites lists the invites, newest first, for an admin.
func GETInvites(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	invites, err := models.ListInvites(db)
	if err != nil {
		slog.Error("Error listing the invites", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	callsigns, err := inviteCallsigns(db, invites)
	if err != nil {
		slog.Error("Error listing the invites", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	out := apimodels.InviteList{Invites: make([]apimodels.Invite, 0, len(invites))}
	for _, inv := range invites {
		out.Invites = append(out.Invites, apimodels.NewInvite(inv, callsigns))
	}
	c.JSON(http.StatusOK, out)
}

func inviteCallsigns(db *gorm.DB, invites []models.Invite) (map[uint]string, error) {
	var ids []uint
	for _, inv := range invites {
		ids = append(ids, inv.CreatedBy)
		if inv.UsedBy != nil {
			ids = append(ids, *inv.UsedBy)
		}
	}
	out := map[uint]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var users []models.User
	if err := db.Unscoped().Select("id", "callsign").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("could not find the users of the invites: %w", err)
	}
	for _, u := range users {
		out[u.ID] = u.Callsign
	}
	return out, nil
}

// POSTInvite makes an invite for an admin, and answers its code and link
// once.
func POSTInvite(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	by, _ := sessions.Default(c).Get("user_id").(uint)
	inv, code, err := models.CreateInvite(db, by, time.Now())
	if err != nil {
		slog.Error("Error making an invite", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	callsigns, err := inviteCallsigns(db, []models.Invite{inv})
	if err != nil {
		slog.Error("Error making an invite", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	c.JSON(http.StatusCreated, apimodels.NewInviteCode{
		Invite: apimodels.NewInvite(inv, callsigns),
		Code:   code,
		Link:   strings.TrimRight(config.HTTP.CanonicalHost, "/") + "/register#invite=" + code,
	})
}

// DELETEInvite deletes an unused invite, for an admin.
func DELETEInvite(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgInviteNotFound})
		return
	}
	done, err := models.DeleteUnusedInvite(db, uint(id))
	if err != nil {
		slog.Error("Error deleting an invite", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	if !done {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgInviteNotFound})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "Invite deleted"})
}

const msgInviteNotFound = "No unused invite has this ID"
