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
	"fmt"
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
	closed, err := closedFor(c)
	if err != nil {
		slog.Error("Failed to check for an admin", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgInternalServerError})
		return
	}
	c.JSON(http.StatusOK, gin.H{"setupwizard": true, "config_valid": valid, "first_admin_open": open, closedKey: closed})
}

const (
	closedKey      = "closed"
	closedDatabase = "database"
	closedSettings = "settings"
	msgClosedDB    = "DMRHub cannot reach its database, and a database setting was saved before, so the setup wizard cannot change it. An admin must fix the database in the config file, the environment or the flags, then restart DMRHub."
	msgClosedSet   = "DMRHub has an admin, so the setup wizard cannot change the settings. An admin must fix them in the config file, the environment or the flags, then restart DMRHub."
)

// closedFor returns why the setup wizard can change no setting: a
// database setting saved before that DMRHub cannot reach, or an admin that
// exists. It returns "" while the wizard is open.
func closedFor(c *gin.Context) (string, error) {
	p := phaseOf(c)
	if p == nil {
		return "", nil
	}
	if fix := p.Fix(); fix != nil {
		if fix.Closed() {
			return closedDatabase, nil
		}
		return "", nil
	}
	if p.DB() == nil {
		return "", nil
	}
	db := p.DB().WithContext(c.Request.Context())
	open, err := models.FirstAdminOpen(db)
	if err != nil {
		return "", fmt.Errorf("failed to check for the first admin: %w", err)
	}
	if !open {
		return closedSettings, nil
	}
	admins, err := models.CountUserAdmins(db)
	if err != nil {
		return "", fmt.Errorf("failed to count the admins: %w", err)
	}
	if admins > 0 {
		return closedSettings, nil
	}
	return "", nil
}

// RefuseClosed refuses a request for the settings once the setup wizard
// can change none: a visitor of the wizard is not an admin.
func RefuseClosed(c *gin.Context) {
	closed, err := closedFor(c)
	switch {
	case err != nil:
		slog.Error("Failed to check for an admin", "error", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{errorKey: msgInternalServerError})
	case closed == closedDatabase:
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{errorKey: msgClosedDB, closedKey: closed})
	case closed == closedSettings:
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{errorKey: msgClosedSet, closedKey: closed})
	default:
		c.Next()
	}
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
