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

package middleware

import (
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/gin-gonic/gin"
)

func MakeDB() gin.HandlerFunc {
	return func(c *gin.Context) {
		config, ok := c.MustGet("Config").(*config.Config)
		if !ok {
			c.AbortWithStatusJSON(500, gin.H{"error": "Unable to get DB config from context"})
			return
		}
		db, err := db.MakeDB(config)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{"error": "Unable to connect to database: " + err.Error()})
			return
		}
		c.Set("DB", db)
		c.Next()
	}
}
