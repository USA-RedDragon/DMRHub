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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	authctl "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/auth"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const msgErrRadioIDDrift = "Error reading the RadioID drift"

// GETRadioIDDrifts lists the users whose callsign RadioID did not give
// their DMR ID at the last check, for an admin.
func GETRadioIDDrifts(c *gin.Context) {
	pdb, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	rows, err := models.ListRadioIDDrifts(pdb)
	if err != nil {
		slog.Error(msgErrRadioIDDrift, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrRadioIDDrift})
		return
	}
	total, err := models.CountRadioIDDrifts(db)
	if err != nil {
		slog.Error(msgErrRadioIDDrift, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrRadioIDDrift})
		return
	}
	out := apimodels.RadioIDDriftList{Total: total, Drifts: make([]apimodels.RadioIDDrift, 0, len(rows))}
	for _, r := range rows {
		listed := listedCallsign(r.UserID)
		out.Drifts = append(out.Drifts, apimodels.RadioIDDrift{
			RadioID:         r.RadioID,
			UserID:          r.UserID,
			Username:        r.Username,
			Callsign:        r.Callsign,
			RadioIDCallsign: r.RadioIDCallsign,
			FoundAt:         r.FoundAt,
			CheckedAt:       r.CheckedAt,
			CanFix:          r.RadioIDCallsign != "" && listed != "" && strings.EqualFold(r.RadioIDCallsign, listed) && !strings.EqualFold(listed, r.Callsign),
			CanNotify:       config.SMTP.Enabled && r.EmailVerified && r.Email != "",
		})
	}
	c.JSON(http.StatusOK, out)
}

// listedCallsign returns the callsign that RadioID lists for id, in upper
// case, or empty when RadioID does not have id.
func listedCallsign(id uint) string {
	entry, found := userdb.Get(id)
	if !found || entry.ID != id {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(entry.Callsign))
}

// POSTRadioIDDriftFix gives the user of the radio ID :radio_id the
// callsign that RadioID lists for their account ID now, for an admin. For
// an added radio ID, RadioID must list it under that callsign too. It
// takes the radio IDs of the user that RadioID lists under the callsign
// off the RadioID drift list. With notify, it emails the user when it can.
func POSTRadioIDDriftFix(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	ridInt, err := strconv.ParseUint(c.Param("radio_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid radio ID"})
		return
	}
	rid := uint(ridInt)
	var json apimodels.RadioIDDriftFix
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&json); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
			return
		}
	}
	if !config.DMR.Checks().Drift() {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Radio ID validation is off"})
		return
	}
	id, owned, err := models.RadioIDOwner(db, rid)
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return
	}
	if !owned {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "No user has this radio ID"})
		return
	}
	user, err := models.FindUserWithoutRepeaters(db, id)
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return
	}
	callsign := listedCallsign(id)
	if callsign == "" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "RadioID does not have this DMR ID"})
		return
	}
	if rid != id && listedCallsign(rid) != callsign {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "RadioID lists this radio ID under another callsign than the DMR ID of the user"})
		return
	}
	if callsign != user.Callsign {
		taken, err := models.UsernameIsCallsign(db, callsign, id)
		if err != nil {
			slog.Error(msgErrFindingUser, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
			return
		}
		if taken {
			c.JSON(http.StatusConflict, gin.H{errorKey: "Another user has the callsign " + callsign})
			return
		}
		if !checkUsernameAndCallsign(c, db, id, "", callsign) {
			return
		}
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if callsign != user.Callsign {
			if err := tx.Model(&models.User{}).Where("id = ?", id).UpdateColumn("callsign", callsign).Error; err != nil {
				return fmt.Errorf("could not set the callsign of %d: %w", id, err)
			}
		}
		if err := models.SetCallsignOverride(tx, id, false); err != nil {
			return fmt.Errorf("could not fix user %d: %w", id, err)
		}
		if err := models.DeleteRadioIDDrift(tx, id); err != nil {
			return fmt.Errorf("could not fix user %d: %w", id, err)
		}
		if err := models.ResolveRadioIDDrifts(tx, id, callsign); err != nil {
			return fmt.Errorf("could not fix user %d: %w", id, err)
		}
		return nil
	})
	if err != nil {
		slog.Error("Error fixing a RadioID drift", "user", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error updating user"})
		return
	}
	notified := json.Notify && callsign != user.Callsign && notifyCallsign(c, user, callsign)
	c.JSON(http.StatusOK, apimodels.RadioIDDriftFixed{Message: "User updated", Callsign: callsign, Notified: notified})
}

func notifyCallsign(c *gin.Context, user models.User, callsign string) bool {
	config, ok := utils.GetConfig(c)
	if !ok || !config.SMTP.Enabled || !user.EmailVerified || user.Email == "" {
		return false
	}
	mailer, ok := c.Value(authctl.MailerKey).(smtp.Mailer)
	if !ok {
		slog.Error("Mailer not found in context, no callsign notice is sent", "user", user.ID)
		return false
	}
	subject := "Your " + config.NetworkName + " callsign is now " + callsign
	body := fmt.Sprintf("RadioID gives your DMR ID %d the callsign %s, so an admin of %s changed your callsign there from %s to %s.\n\n"+
		"Log in with your username or your new callsign.\n",
		user.ID, callsign, config.NetworkName, user.Callsign, callsign)
	go func() {
		if err := mailer.SendText(user.Email, subject, body); err != nil {
			slog.Error("Could not send a callsign notice", "user", user.ID, "error", err)
		}
	}()
	return true
}
