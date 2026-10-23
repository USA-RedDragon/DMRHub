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

package config

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
)

const (
	errorKey  = "error"
	errorsKey = "errors"
)

// ApplierKey is the key of the Applier in the gin context.
const ApplierKey = "ConfigApplier"

// Applier puts a config that PUTConfig saves into effect while DMRHub
// runs. For each part that cannot take its new settings, Apply sets the
// settings of that part in next back to the ones it runs with and
// returns why.
type Applier interface {
	Apply(next *config.Config) []manager.ApplyError
}

func PUTConfig(c *gin.Context) {
	var req apimodels.POSTConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}

	currentCfg, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	nextCfg := &config.Config{}
	req.ToConfig(nextCfg, currentCfg)

	errs := nextCfg.ValidateWithFields()
	if len(errs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid configuration", errorsKey: errs})
		return
	}
	var applyErrs []manager.ApplyError
	if applier, ok := c.Get(ApplierKey); ok {
		if a, ok := applier.(Applier); ok {
			applyErrs = a.Apply(nextCfg)
		}
	}
	*currentCfg = *nextCfg
	if err := currentCfg.Save(); err != nil {
		slog.Error("Failed to save config", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Failed to save config: " + err.Error()})
		return
	}

	if len(applyErrs) > 0 {
		c.JSON(http.StatusConflict, gin.H{errorKey: "Some settings could not be applied", "applyErrors": formatApplyErrors(applyErrs)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Configuration updated successfully"})
}

func formatApplyErrors(errs []manager.ApplyError) []gin.H {
	out := make([]gin.H, 0, len(errs))
	for _, e := range errs {
		out = append(out, gin.H{"server": string(e.Server), "running": e.Running, errorKey: e.Err.Error()})
	}
	return out
}

func GETConfig(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	response := apimodels.ConfigResponse{
		Config: *config,
		Secrets: apimodels.SecretStatus{
			SecretSet:       config.Secret != "",
			PasswordSaltSet: config.PasswordSalt != "",
			SMTPPasswordSet: config.SMTP.Password != "",
			APRSPasscodeSet: config.DMR.Positions.APRSIS.Passcode != "",
		},
	}

	c.JSON(http.StatusOK, response)
}

func GETConfigValidate(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	errs := config.ValidateWithFields()

	c.JSON(http.StatusOK, gin.H{"valid": len(errs) == 0, errorsKey: formatErrors(errs)})
}

func POSTConfigValidate(c *gin.Context) {
	var req apimodels.POSTConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}
	currentCfg, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	nextCfg := &config.Config{}
	req.ToConfig(nextCfg, currentCfg)

	errs := nextCfg.ValidateWithFields()

	c.JSON(http.StatusOK, gin.H{"valid": len(errs) == 0, errorsKey: formatErrors(errs)})
}

func formatErrors(errs []config.ValidationError) map[string]any {
	errors := make(map[string]any)

	for _, e := range errs {
		// Field is a string, but may contain dots for nested fields
		// e.g. "smtp.password"
		// We need to split by the dot and create sub-maps as needed
		parts := strings.Split(e.Field, ".")
		currentMap := errors
		for i, part := range parts {
			if i == len(parts)-1 {
				// Last part, set the error message
				currentMap[part] = e.Err.Error()
			} else {
				// Not last part, create a new map if it doesn't exist
				if _, ok := currentMap[part]; !ok {
					currentMap[part] = make(map[string]any)
				}
				// Move to the next map
				nextMap, ok := currentMap[part].(map[string]any)
				if !ok {
					// This should never happen, but just in case
					break
				}
				currentMap = nextMap
			}
		}

		// Set the error message for the last part
		currentMap[parts[len(parts)-1]] = e.Err.Error()
	}

	return errors
}
