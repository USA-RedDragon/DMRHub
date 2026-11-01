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
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
)

const (
	msgBadDelay   = "Pick a delay of 5, 10, 30 or 60 minutes"
	msgNoEmail    = "Verify an email address in Settings first"
	msgNoSMTP     = "This network does not send email"
	defaultDelay  = 10
	visibleLetter = 1
)

func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= visibleLetter {
		return email
	}
	return email[:visibleLetter] + "***" + email[at:]
}

func offlineAlertOf(c *gin.Context) (apimodels.OfflineAlert, models.User, models.Repeater, bool) {
	db, ok := utils.GetDB(c)
	if !ok {
		return apimodels.OfflineAlert{}, models.User{}, models.Repeater{}, false
	}
	cfg, ok := utils.GetConfig(c)
	if !ok {
		return apimodels.OfflineAlert{}, models.User{}, models.Repeater{}, false
	}
	user, ok := sessionUser(c, db)
	if !ok {
		return apimodels.OfflineAlert{}, models.User{}, models.Repeater{}, false
	}
	rpt, ok := cosysopRepeater(c, db)
	if !ok {
		return apimodels.OfflineAlert{}, models.User{}, models.Repeater{}, false
	}
	out := apimodels.OfflineAlert{SMTP: cfg.SMTP.Enabled, EmailVerified: user.Email != "" && user.EmailVerified, DelayMinutes: defaultDelay, Delays: models.OfflineAlertDelays()}
	if out.EmailVerified {
		out.Email = maskEmail(user.Email)
	}
	a, found, err := models.FindOfflineAlert(db, user.ID, rpt.ID)
	if err != nil {
		slog.Error("Error reading an offline alert", "repeaterID", rpt.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return out, user, rpt, false
	}
	if found {
		out.Enabled, out.DelayMinutes, out.Back = true, a.DelayMinutes, a.Back
	}
	return out, user, rpt, true
}

// GETRepeaterAlert returns the offline alert of the user for the repeater.
func GETRepeaterAlert(c *gin.Context) {
	out, _, _, ok := offlineAlertOf(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, out)
}

// PUTRepeaterAlert turns the offline alert of the user for the repeater on
// or off. Turning it on needs SMTP and a verified email address.
func PUTRepeaterAlert(c *gin.Context) {
	out, user, rpt, ok := offlineAlertOf(c)
	if !ok {
		return
	}
	var put apimodels.OfflineAlertPut
	if err := c.ShouldBindJSON(&put); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	db, _ := utils.GetDB(c)
	if !put.Enabled {
		if err := models.DeleteOfflineAlert(db, user.ID, rpt.ID); err != nil {
			slog.Error("Error turning off an offline alert", "repeaterID", rpt.ID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
			return
		}
		out.Enabled = false
		c.JSON(http.StatusOK, out)
		return
	}
	switch {
	case !models.ValidOfflineAlertDelay(put.DelayMinutes):
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadDelay})
		return
	case !out.SMTP:
		c.JSON(http.StatusConflict, gin.H{errorKey: msgNoSMTP})
		return
	case !out.EmailVerified:
		c.JSON(http.StatusConflict, gin.H{errorKey: msgNoEmail})
		return
	}
	if err := models.SetOfflineAlert(db, user.ID, rpt.ID, put.DelayMinutes, put.Back, time.Now()); err != nil {
		slog.Error("Error saving an offline alert", "repeaterID", rpt.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	out.Enabled, out.DelayMinutes, out.Back = true, put.DelayMinutes, put.Back
	c.JSON(http.StatusOK, out)
}
