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

package v1

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/gin-gonic/gin"
)

func GETNetworkName(c *gin.Context) {
	config, ok := c.MustGet("Config").(*config.Config)
	if !ok {
		slog.Error("Unable to get Config from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	_, err := io.WriteString(c.Writer, config.NetworkName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting network name"})
	}
}

func GETVersion(c *gin.Context) {
	version, ok := c.MustGet("Version").(string)
	if !ok {
		logging.Errorf("Unable to get Version from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	commit, ok := c.MustGet("Commit").(string)
	if !ok {
		logging.Errorf("Unable to get Commit from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	_, err := io.WriteString(c.Writer, fmt.Sprintf("%s-%s", version, commit))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting version"})
	}
}

func GETPing(c *gin.Context) {
	_, err := io.WriteString(c.Writer, fmt.Sprintf("%d", time.Now().Unix()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting ping"})
	}
}
