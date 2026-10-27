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
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	configcontroller "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/config"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// StateKey is the key of the Phase of the setup wizard in the gin context.
const StateKey = "SetupWizardState"

// Phase is the state of the setup wizard: a database fix while DMRHub
// cannot reach its database, then the Live and the database of DMRHub.
type Phase interface {
	Fix() *liveconfig.DatabaseFix
	Live() *liveconfig.Live
	DB() *gorm.DB
}

func phaseOf(c *gin.Context) Phase {
	p, _ := c.Value(StateKey).(Phase)
	return p
}

// databaseFix returns the database fix of the wizard, or nil after the
// database phase.
func databaseFix(c *gin.Context) *liveconfig.DatabaseFix {
	if p := phaseOf(c); p != nil {
		return p.Fix()
	}
	return nil
}

// GETConfig serves the config as the config API does, and, while DMRHub
// cannot reach its database, the config of the deployment with why.
func GETConfig(c *gin.Context) {
	fix := databaseFix(c)
	if fix == nil {
		configcontroller.GETConfig(c)
		return
	}
	problem := fix.Problem()
	c.JSON(http.StatusOK, apimodels.ConfigResponse{
		Config: *fix.Base,
		Secrets: apimodels.SecretStatus{
			SecretSet:       fix.Base.Secret != "",
			PasswordSaltSet: fix.Base.PasswordSalt != "",
			SMTPPasswordSet: fix.Base.SMTP.Password != "",
		},
		Status: liveconfig.PendingStatus(fix.Locks, problem.StoreError == ""),
		Wizard: &apimodels.WizardStatus{Phase: liveconfig.PhaseDatabase, Problem: problem},
	})
}

// POSTConfigValidate checks a config as the config API does, and, while
// DMRHub cannot reach its database, checks that it changes only the
// database settings.
func POSTConfigValidate(c *gin.Context) {
	fix := databaseFix(c)
	if fix == nil {
		configcontroller.POSTConfigValidate(c)
		return
	}
	var req apimodels.POSTConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}
	next := &config.Config{}
	req.ToConfig(next, fix.Base)
	errs := fix.Check(next)
	c.JSON(http.StatusOK, gin.H{"valid": len(errs) == 0, errorsKey: configcontroller.FormatErrors(errs)})
}

// PUTConfig saves a config as the config API does, and, while DMRHub
// cannot reach its database, tries the new database setting. When DMRHub
// reaches the database, it keeps the setting in its DatabaseState and
// answers with the phase of the wizard that comes next.
func PUTConfig(c *gin.Context) {
	fix := databaseFix(c)
	if fix == nil {
		configcontroller.PUTConfig(c)
		return
	}
	var req apimodels.POSTConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}
	next := &config.Config{}
	req.ToConfig(next, fix.Base)
	if errs := fix.Check(next); len(errs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid configuration", errorsKey: configcontroller.FormatErrors(errs)})
		return
	}
	phase, err := fix.Fix(c.Request.Context(), next.Database)
	if err != nil {
		field := "database.host"
		if next.Database.Driver == config.DatabaseDriverSQLite {
			field = "database.database"
		}
		c.JSON(http.StatusBadRequest, gin.H{
			errorKey:  err.Error(),
			errorsKey: configcontroller.FormatErrors([]config.ValidationError{{Field: field, Err: err}}),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "DMRHub reached the database", "phase": phase})
}

// ProvideDB gives the request the database of the setup phase, and
// answers 503 before DMRHub reaches its database.
func ProvideDB(c *gin.Context) {
	p := phaseOf(c)
	var db *gorm.DB
	if p != nil {
		db = p.DB()
	}
	if db == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{errorKey: "DMRHub cannot reach its database yet"})
		return
	}
	c.Set("DB", db.WithContext(c.Request.Context()))
	c.Next()
}
