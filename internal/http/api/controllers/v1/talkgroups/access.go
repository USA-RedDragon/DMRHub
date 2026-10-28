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

package talkgroups

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	msgErrAccess       = "Error getting the access of the talkgroup"
	msgErrSavingAccess = "Error saving the access of the talkgroup"
	msgBadAccessMode   = "Mode must be open or approved"
	msgNoSuchRepeater  = "Repeater does not exist"
	msgNoSuchTalkgroup = "Talkgroup does not exist"
	msgBadJSON         = "JSON data is invalid"
)

// GETTalkgroupAccess answers who may link a talkgroup.
func GETTalkgroupAccess(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, ok := accessTalkgroup(c, db)
	if !ok {
		return
	}
	answerAccess(c, db, id)
}

// PATCHTalkgroupAccess changes who may link a talkgroup and answers it.
func PATCHTalkgroupAccess(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, ok := accessTalkgroup(c, db)
	if !ok {
		return
	}
	var patch apimodels.TalkgroupAccessPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadJSON})
		return
	}
	if patch.Mode != nil && *patch.Mode != apimodels.TalkgroupAccessOpen && *patch.Mode != apimodels.TalkgroupAccessApproved {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadAccessMode})
		return
	}
	restricted, repeaters, err := models.TalkgroupAccess(db, id)
	if err != nil {
		slog.Error("Error reading the access of a talkgroup", "talkgroupID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingAccess})
		return
	}
	if patch.Mode != nil {
		restricted = *patch.Mode == apimodels.TalkgroupAccessApproved
	}
	if patch.Repeaters != nil {
		repeaters = slices.Compact(slices.Sorted(slices.Values(*patch.Repeaters)))
		var n int64
		if len(repeaters) > 0 {
			if err := db.Model(&models.Repeater{}).Where("id IN ?", repeaters).Count(&n).Error; err != nil {
				slog.Error("Error finding repeaters", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingAccess})
				return
			}
		}
		if n != int64(len(repeaters)) {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: msgNoSuchRepeater})
			return
		}
	}
	if err := models.SetTalkgroupAccess(db, id, restricted, repeaters); err != nil {
		slog.Error("Error saving the access of a talkgroup", "talkgroupID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingAccess})
		return
	}
	if h, ok := dmrHubOf(c); ok {
		h.RulesChanged(id)
	}
	answerAccess(c, db, id)
}

func accessTalkgroup(c *gin.Context, db *gorm.DB) (uint, bool) {
	id64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid talkgroup ID"})
		return 0, false
	}
	id := uint(id64)
	exists, err := models.TalkgroupIDExists(db, id)
	if err != nil {
		slog.Error("Error finding a talkgroup", "talkgroupID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrAccess})
		return 0, false
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoSuchTalkgroup})
		return 0, false
	}
	return id, true
}

func answerAccess(c *gin.Context, db *gorm.DB, id uint) {
	restricted, ids, err := models.TalkgroupAccess(db, id)
	if err != nil {
		slog.Error("Error reading the access of a talkgroup", "talkgroupID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrAccess})
		return
	}
	out := apimodels.TalkgroupAccess{Mode: apimodels.TalkgroupAccessOpen, Repeaters: []apimodels.RepeaterRef{}}
	if restricted {
		out.Mode = apimodels.TalkgroupAccessApproved
	}
	if len(ids) > 0 {
		var rpts []models.Repeater
		if err := db.Select("id", "callsign").Where("id IN ?", ids).Order("id").Find(&rpts).Error; err != nil {
			slog.Error("Error finding repeaters", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrAccess})
			return
		}
		for _, r := range rpts {
			out.Repeaters = append(out.Repeaters, apimodels.RepeaterRef{ID: r.ID, Callsign: r.Callsign})
		}
	}
	c.JSON(http.StatusOK, out)
}
