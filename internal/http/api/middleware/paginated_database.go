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
	"math"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/pagination"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type PaginationConfig struct {
	DefaultLimit int
	MaxLimit     int
	// AllRows are the routes, as "GET /api/v1/talkgroups", that answer
	// every row for limit=none. Nil is allRowsRoutes. Every other route
	// takes limit=none as MaxLimit.
	AllRows map[string]bool
}

// allRowsRoutes are the lists that the web pages and dmr-web read whole
// with limit=none: the talkgroups and users to pick from, and the
// hotspots of the user.
func allRowsRoutes() map[string]bool {
	return map[string]bool{
		"GET /api/v1/talkgroups":   true,
		"GET /api/v1/users":        true,
		"GET /api/v1/repeaters/my": true,
	}
}

func PaginatedDatabaseProvider(db *gorm.DB, config PaginationConfig) gin.HandlerFunc {
	if config.MaxLimit == 0 {
		config.MaxLimit = 100
	}
	if config.DefaultLimit == 0 {
		config.DefaultLimit = 10
	}
	if config.AllRows == nil {
		config.AllRows = allRowsRoutes()
	}
	return func(c *gin.Context) {
		var limit int
		limitStr, exists := c.GetQuery("limit")
		all := limitStr == "none" && config.AllRows[c.Request.Method+" "+c.FullPath()]
		switch {
		case !exists:
			limit = config.DefaultLimit
		case all:
			limit = math.MaxInt32
		case limitStr == "none":
			limit = config.MaxLimit
		default:
			var err error
			limit, err = strconv.Atoi(limitStr)
			if err != nil {
				limit = config.DefaultLimit
			}
		}

		if !all && limit > config.MaxLimit {
			limit = config.MaxLimit
		}
		if limit < 1 {
			limit = 1
		}

		var page int
		pageStr, exists := c.GetQuery("page")
		if !exists {
			page = 1
		} else {
			var err error
			page, err = strconv.Atoi(pageStr)
			if err != nil {
				// Bad page, use default
				page = 1
			}
		}

		if page < 1 {
			page = 1
		}

		c.Set("PaginatedDB",
			db.WithContext(c.Request.Context()).Scopes(pagination.NewPaginate(limit, page).Paginate),
		)
		c.Next()
	}
}
