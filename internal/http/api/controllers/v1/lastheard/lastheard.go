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

package lastheard

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	errorKey         = "error"
	totalKey         = "total"
	msgTryAgainLater = "Try again later"
	callsKey         = "calls"
)

type query struct {
	db     *gorm.DB
	count  *gorm.DB
	viewer *models.User
}

func viewerOf(c *gin.Context, db *gorm.DB) *models.User {
	id, ok := sessions.Default(c).Get("user_id").(uint)
	if !ok {
		return nil
	}
	user, err := models.FindUserByID(db, id)
	if err != nil {
		return nil
	}
	return &user
}

func start(c *gin.Context) (query, bool) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return query{}, false
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return query{}, false
	}
	idle, _ := strconv.ParseBool(c.Query("idle"))
	filter, err := models.ParseCallFilter(c.Query("types"), idle)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid transmission type"})
		return query{}, false
	}
	viewer := viewerOf(c, cDb)
	scope := filter.Scope(cDb, viewer, models.CurrentSMSWindow())
	return query{db: db.Scopes(scope), count: cDb.Scopes(scope), viewer: viewer}, true
}

func (q query) respond(c *gin.Context, calls []models.Call, findErr error, count int, countErr error) {
	if findErr != nil {
		slog.Error("Unable to find calls", "error", findErr)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	if countErr != nil {
		slog.Error("Unable to count calls", "error", countErr)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}
	rows, err := apimodels.NewWSCallResponsesFor(cDb, q.viewer, models.CurrentSMSWindow(), calls)
	if err != nil {
		slog.Error("Unable to show calls", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	c.JSON(http.StatusOK, gin.H{callsKey: rows, totalKey: count})
}

func parseID(c *gin.Context, what string) (uint, bool) {
	id64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid " + what + " ID"})
		return 0, false
	}
	return uint(id64), true
}

func GETLastheard(c *gin.Context) {
	q, ok := start(c)
	if !ok {
		return
	}
	calls, findErr := models.FindCalls(q.db)
	count, countErr := models.CountCalls(q.count)
	q.respond(c, calls, findErr, count, countErr)
}

func GETLastheardUser(c *gin.Context) {
	userID, ok := parseID(c, "User")
	if !ok {
		return
	}
	q, ok := start(c)
	if !ok {
		return
	}
	calls, findErr := models.FindUserCalls(q.db, userID)
	count, countErr := models.CountUserCalls(q.count, userID)
	q.respond(c, calls, findErr, count, countErr)
}

func GETLastheardRepeater(c *gin.Context) {
	repeaterID, ok := parseID(c, "Repeater")
	if !ok {
		return
	}
	q, ok := start(c)
	if !ok {
		return
	}
	calls, findErr := models.FindRepeaterCalls(q.db, repeaterID)
	count, countErr := models.CountRepeaterCalls(q.count, repeaterID)
	q.respond(c, calls, findErr, count, countErr)
}

func GETLastheardTalkgroup(c *gin.Context) {
	talkgroupID, ok := parseID(c, "Talkgroup")
	if !ok {
		return
	}
	q, ok := start(c)
	if !ok {
		return
	}
	calls, findErr := models.FindTalkgroupCalls(q.db, talkgroupID)
	count, countErr := models.CountTalkgroupCalls(q.count, talkgroupID)
	q.respond(c, calls, findErr, count, countErr)
}
