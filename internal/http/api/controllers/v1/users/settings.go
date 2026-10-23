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
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const msgTryLater = "Try again later"

// GETUserSettings returns the settings of the logged-in user.
func GETUserSettings(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, apimodels.UserSettings{SMSStorage: user.SMSStorageEnabled})
}

// PUTUserSMSStorage turns SMS Storage of the logged-in user on or off.
func PUTUserSMSStorage(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	var req apimodels.SMSStoragePut
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Send enabled as true or false"})
		return
	}
	if err := models.SetSMSStorage(db, &user, *req.Enabled, time.Now()); err != nil {
		slog.Error("Error saving SMS Storage", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	c.JSON(http.StatusOK, apimodels.UserSettings{SMSStorage: user.SMSStorageEnabled})
}

// GETMyRadioIDs lists the radio IDs of the logged-in user.
func GETMyRadioIDs(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	listRadioIDs(c, db, &user)
}

// POSTMyRadioID adds a radio ID to the logged-in user after the user
// database shows that it is registered to their callsign.
func POSTMyRadioID(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	if config.DMR.DisableRadioIDValidation {
		c.JSON(http.StatusForbidden, gin.H{errorKey: "Only an admin can add radio IDs on this hub"})
		return
	}
	addRadioID(c, db, &user, true)
}

// DELETEMyRadioID removes a radio ID of the logged-in user.
func DELETEMyRadioID(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	deleteRadioID(c, db, &user)
}

// GETUserRadioIDs lists the radio IDs of the user :id, for an admin.
func GETUserRadioIDs(c *gin.Context) {
	db, user, ok := pathUser(c)
	if !ok {
		return
	}
	listRadioIDs(c, db, &user)
}

// POSTUserRadioID adds a radio ID to the user :id, for an admin, without
// the user database check.
func POSTUserRadioID(c *gin.Context) {
	db, user, ok := pathUser(c)
	if !ok {
		return
	}
	addRadioID(c, db, &user, false)
}

// DELETEUserRadioID removes a radio ID of the user :id, for an admin.
func DELETEUserRadioID(c *gin.Context) {
	db, user, ok := pathUser(c)
	if !ok {
		return
	}
	deleteRadioID(c, db, &user)
}

func pathUser(c *gin.Context) (*gorm.DB, models.User, bool) {
	db, ok := utils.GetDB(c)
	if !ok {
		return nil, models.User{}, false
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return nil, models.User{}, false
	}
	var user models.User
	err = db.First(&user, uint(id)).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgUserNotFound})
		return nil, models.User{}, false
	case err != nil:
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return nil, models.User{}, false
	}
	return db, user, true
}

func callsignOf(id uint, fallback string) string {
	if u, ok := userdb.Get(id); ok && u.Callsign != "" {
		return u.Callsign
	}
	return fallback
}

func listRadioIDs(c *gin.Context, db *gorm.DB, user *models.User) {
	ids, err := models.ListUserRadioIDs(db, user.ID)
	if err != nil {
		slog.Error("Error listing radio IDs", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	out := apimodels.RadioIDList{RadioIDs: make([]apimodels.RadioID, 0, len(ids)+1)}
	out.RadioIDs = append(out.RadioIDs, apimodels.RadioID{ID: user.ID, Callsign: user.Callsign, Account: true})
	for _, r := range ids {
		at := r.VerifiedAt.UTC()
		out.RadioIDs = append(out.RadioIDs, apimodels.RadioID{ID: r.RadioID, Callsign: callsignOf(r.RadioID, user.Callsign), VerifiedAt: &at})
	}
	c.JSON(http.StatusOK, out)
}

func addRadioID(c *gin.Context, db *gorm.DB, user *models.User, verify bool) {
	var req apimodels.RadioIDPost
	if err := c.ShouldBindJSON(&req); err != nil || !userdb.IsValidUserID(req.ID) {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Enter a 7-digit DMR ID"})
		return
	}
	owner, owned, err := models.RadioIDOwner(db, req.ID)
	if err != nil {
		slog.Error("Error finding the owner of a radio ID", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	switch {
	case owned && owner == user.ID:
		c.JSON(http.StatusBadRequest, gin.H{errorKey: fmt.Sprintf("%d is already on your list", req.ID)})
		return
	case owned:
		c.JSON(http.StatusBadRequest, gin.H{errorKey: fmt.Sprintf("%d belongs to another DMRHub user", req.ID)})
		return
	case verify && !userdb.ValidUserCallsign(req.ID, user.Callsign):
		c.JSON(http.StatusBadRequest, gin.H{errorKey: fmt.Sprintf("RadioID.net does not list %d under your callsign", req.ID)})
		return
	}
	row := models.UserRadioID{UserID: user.ID, RadioID: req.ID, VerifiedAt: time.Now().UTC()}
	if err := db.Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: fmt.Sprintf("%d belongs to another DMRHub user", req.ID)})
			return
		}
		if again, ok2, err2 := models.RadioIDOwner(db, req.ID); err2 == nil && ok2 && again != user.ID {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: fmt.Sprintf("%d belongs to another DMRHub user", req.ID)})
			return
		}
		slog.Error("Error adding a radio ID", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	at := row.VerifiedAt
	c.JSON(http.StatusCreated, apimodels.RadioID{ID: row.RadioID, Callsign: callsignOf(row.RadioID, user.Callsign), VerifiedAt: &at})
}

func deleteRadioID(c *gin.Context, db *gorm.DB, user *models.User) {
	id, err := strconv.ParseUint(c.Param("radio_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "Radio ID not found"})
		return
	}
	if uint(id) == user.ID {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot remove your account ID"})
		return
	}
	res := db.Where("user_id = ? AND radio_id = ?", user.ID, uint(id)).Delete(&models.UserRadioID{})
	if res.Error != nil {
		slog.Error("Error removing a radio ID", "error", res.Error)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "Radio ID not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "Radio ID removed"})
}
