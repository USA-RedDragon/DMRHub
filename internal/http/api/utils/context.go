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

package utils

import (
	"log/slog"
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetDB extracts the "DB" value from the gin context. On failure it writes
// an HTTP 500 response and returns nil, false.
func GetDB(c *gin.Context) (*gorm.DB, bool) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return nil, false
	}
	return db, true
}

// GetPaginatedDB extracts the "PaginatedDB" value from the gin context. On
// failure it writes an HTTP 500 response and returns nil, false.
func GetPaginatedDB(c *gin.Context) (*gorm.DB, bool) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return nil, false
	}
	return db, true
}

// GetConfig extracts the "Config" value from the gin context. On failure it
// writes an HTTP 500 response and returns nil, false.
func GetConfig(c *gin.Context) (*config.Config, bool) {
	cfg, ok := c.MustGet("Config").(*config.Config)
	if !ok {
		slog.Error("Unable to get Config from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return nil, false
	}
	return cfg, true
}
