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
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// POSTUserRequirePasswordChange makes the user of the path choose a new
// password before they use the rest of DMRHub, and logs them out of
// every session, websocket and app. With generate_password, it also sets
// a new random password and answers it in "password", the only place
// that shows it. Only the System Admin may do this to an admin, and no
// one to themselves.
func POSTUserRequirePasswordChange(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	actorID, ok := sessions.Default(c).Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgNotLoggedIn})
		return
	}
	var json apimodels.RequirePasswordChange
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	if uint(userID) == actorID {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot do this to yourself. Change your password in Settings."})
		return
	}
	user, err := models.FindUserWithoutRepeaters(db, uint(userID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgUserNotFound})
		return
	}
	if err != nil {
		slog.Error(msgErrFindingUser, "function", "POSTUserRequirePasswordChange", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return
	}
	if !mayResetPassword(c, db, actorID, user) {
		return
	}

	password, ok := markPasswordChange(c, db, config.PasswordSalt, user.ID, json.GeneratePassword)
	if !ok {
		return
	}
	if !utils.RevokeUserGrants(c, user.ID) {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "The password must change, but the apps of the user could not be signed out"})
		return
	}
	if !endOtherSessions(c, user.ID) {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "The password must change, but the sessions of the user could not be ended"})
		return
	}
	out := gin.H{messageKey: "The user must choose a new password"}
	if password != "" {
		c.Header("Cache-Control", "no-store")
		out["password"] = password
	}
	c.JSON(http.StatusOK, out)
}

// markPasswordChange makes the user id choose a new password and, with
// generate, gives them a new random password, which it returns. It
// answers the request and returns false on an error.
func markPasswordChange(c *gin.Context, db *gorm.DB, salt string, id uint, generate bool) (string, bool) {
	if !generate {
		if err := models.RequirePasswordChange(db, id); err != nil {
			slog.Error(msgErrSavingUser, "function", "POSTUserRequirePasswordChange", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingUser})
			return "", false
		}
		return "", true
	}
	password, err := utils.TemporaryPassword()
	if err != nil {
		slog.Error("Error making a temporary password", "function", "POSTUserRequirePasswordChange", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return "", false
	}
	hash, err := utils.HashPassword(password, salt)
	if err != nil {
		slog.Error("Error hashing password", "function", "POSTUserRequirePasswordChange", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrHashingPassword})
		return "", false
	}
	if err := models.SetPassword(db, id, hash, true); err != nil {
		slog.Error(msgErrSavingUser, "function", "POSTUserRequirePasswordChange", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingUser})
		return "", false
	}
	return password, true
}
