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

package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type pageRow struct {
	ID uint `gorm:"primaryKey"`
}

func TestLimitNoneAnswersEveryRowOnlyWhereARouteAllowsIt(t *testing.T) {
	t.Parallel()
	database, err := gorm.Open(sqlite.Open(memdb.DSN()), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&pageRow{}))
	rows := make([]pageRow, 150)
	for i := range rows {
		rows[i].ID = uint(i + 1)
	}
	require.NoError(t, database.Create(&rows).Error)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.PaginatedDatabaseProvider(database, middleware.PaginationConfig{}))
	count := func(c *gin.Context) {
		db, ok := utils.GetPaginatedDB(c)
		if !ok {
			return
		}
		var got []pageRow
		if err := db.Find(&got).Error; err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.String(http.StatusOK, strconv.Itoa(len(got)))
	}
	for _, path := range []string{"/api/v1/talkgroups", "/api/v1/users", "/api/v1/repeaters/my", "/api/v1/lastheard", "/api/v1/messages/inbox", "/api/v1/repeaters"} {
		r.GET(path, count)
	}

	for _, tc := range []struct {
		url  string
		rows string
	}{
		{"/api/v1/talkgroups?limit=none", "150"},
		{"/api/v1/users?limit=none", "150"},
		{"/api/v1/repeaters/my?limit=none", "150"},
		{"/api/v1/lastheard?limit=none", "100"},
		{"/api/v1/messages/inbox?limit=none", "100"},
		{"/api/v1/repeaters?limit=none", "100"},
		{"/api/v1/lastheard?limit=1000", "100"},
		{"/api/v1/lastheard", "10"},
		{"/api/v1/talkgroups?limit=20", "20"},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.url, nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, tc.url)
		assert.Equal(t, tc.rows, w.Body.String(), tc.url)
	}
}
