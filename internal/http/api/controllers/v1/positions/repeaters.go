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

package positions

import (
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
)

// GETRepeaterPositions returns the positions of the connected MMDVM
// repeaters that report one. A user sees the repeaters that they own or
// are a co-sysop of, as on the Repeaters page, and an admin sees every
// repeater, as on the admin Repeaters page.
func GETRepeaterPositions(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	all := user.Admin && !utils.ViaToken(c)
	repeaters, err := models.ConnectedRepeaterPositions(db, user.ID, all, time.Now())
	if err != nil {
		failed(c, "cannot list repeater positions", err)
		return
	}
	out := apimodels.RepeaterPositionList{Repeaters: make([]apimodels.RepeaterPosition, 0, len(repeaters))}
	for i := range repeaters {
		out.Repeaters = append(out.Repeaters, apimodels.NewRepeaterPosition(&repeaters[i]))
	}
	c.JSON(http.StatusOK, out)
}
