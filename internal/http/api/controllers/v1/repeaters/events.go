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

package repeaters

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
)

// GETRepeaterEvents returns the connection log of a repeater: the entries
// of the last models.RepeaterEventWindow, newest first.
func GETRepeaterEvents(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	rpt, ok := cosysopRepeater(c, db)
	if !ok {
		return
	}
	events, err := models.ListRepeaterEvents(db, rpt.ID, time.Now().Add(-models.RepeaterEventWindow))
	if err != nil {
		slog.Error("Error reading a connection log", "repeaterID", rpt.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}
