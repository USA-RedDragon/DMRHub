// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/gin-gonic/gin"
)

func PUTConfig(c *gin.Context) {
	var req apimodels.POSTConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	errs := req.ValidateWithFields()
	if len(errs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid configuration", "errors": errs})
		return
	}
	config, ok := c.MustGet("Config").(*config.Config)
	if !ok {
		slog.Error("Unable to get Config from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	req.ToConfig(config)
	c.JSON(http.StatusOK, gin.H{"message": "Configuration updated successfully"})
}

func GETConfig(c *gin.Context) {
	config, ok := c.MustGet("Config").(*config.Config)
	if !ok {
		slog.Error("Unable to get Config from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	c.JSON(http.StatusOK, config)
}

func GETConfigValidate(c *gin.Context) {
	config, ok := c.MustGet("Config").(*config.Config)
	if !ok {
		slog.Error("Unable to get Config from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	errs := config.ValidateWithFields()

	resp := gin.H{"valid": len(errs) == 0}
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
				currentMap[part] = e.Error
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
		currentMap[parts[len(parts)-1]] = e.Error
	}
	resp["errors"] = errors

	c.JSON(http.StatusOK, resp)
}

func POSTConfigValidate(c *gin.Context) {
	var req apimodels.POSTConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	config := &config.Config{}
	req.ToConfig(config)

	errs := config.ValidateWithFields()

	resp := gin.H{"valid": len(errs) == 0}
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
				currentMap[part] = e.Error
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
		currentMap[parts[len(parts)-1]] = e.Error
	}
	resp["errors"] = errors

	c.JSON(http.StatusOK, resp)
}
