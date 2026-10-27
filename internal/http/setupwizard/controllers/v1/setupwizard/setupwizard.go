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
package setupwizard

import (
	"log/slog"
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-gonic/gin"
)

const (
	errorKey               = "error"
	errorsKey              = "errors"
	msgInternalServerError = "Internal server error"
)

// GETSetupWizard reports that the setup wizard runs, whether the settings
// are complete, and whether a visitor can still make the first admin.
func GETSetupWizard(c *gin.Context) {
	p := phaseOf(c)
	open, valid := false, false
	if p != nil && p.DB() != nil {
		var err error
		if open, err = models.FirstAdminOpen(p.DB().WithContext(c.Request.Context())); err != nil {
			slog.Error("Failed to check for the first admin", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgInternalServerError})
			return
		}
		valid = p.Live() != nil && p.Live().Current().Get().Validate() == nil
	}
	c.JSON(http.StatusOK, gin.H{"setupwizard": true, "config_valid": valid, "first_admin_open": open})
}

// POSTSetupWizardComplete ends the setup wizard once the settings are
// complete and an admin exists.
func POSTSetupWizardComplete(c *gin.Context) {
	p := phaseOf(c)
	if p == nil || p.DB() == nil || p.Live() == nil {
		c.JSON(http.StatusConflict, gin.H{errorKey: "DMRHub cannot reach its database yet"})
		return
	}
	if err := p.Live().Current().Get().Validate(); err != nil {
		c.JSON(http.StatusConflict, gin.H{errorKey: "The settings are not complete yet"})
		return
	}
	admins, err := models.CountUserAdmins(p.DB().WithContext(c.Request.Context()))
	if err != nil {
		slog.Error("Failed to count the admins", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgInternalServerError})
		return
	}
	if admins == 0 {
		c.JSON(http.StatusConflict, gin.H{errorKey: "Create the first admin first"})
		return
	}
	if v, ok := c.Value("SetupWizardConfigCompleteChan").(chan any); ok {
		select {
		case v <- struct{}{}:
		default:
		}
	} else {
		slog.Warn("SetupWizardConfigCompleteChan not found in context or of wrong type")
	}
	c.JSON(http.StatusOK, gin.H{"message": "Setup wizard marked as complete"})
}
