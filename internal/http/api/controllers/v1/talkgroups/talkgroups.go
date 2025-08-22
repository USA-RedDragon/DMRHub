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

package talkgroups

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxNameLength = 20
const maxDescriptionLength = 240

func GETTalkgroups(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	talkgroups, err := models.ListTalkgroups(db)
	if err != nil {
		slog.Error("Error listing talkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing talkgroups"})
		return
	}

	total, err := models.CountTalkgroups(cDb)
	if err != nil {
		slog.Error("Error counting talkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error counting talkgroups"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": total, "talkgroups": talkgroups})
}

func GETMyTalkgroups(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	session := sessions.Default(c)

	userID := session.Get("user_id")
	if userID == nil {
		slog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		slog.Error("userID cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	talkgroups, err := models.FindTalkgroupsByOwnerID(db, uid)
	if err != nil {
		slog.Error("Error listing talkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing talkgroups"})
		return
	}
	total, err := models.CountTalkgroupsByOwnerID(cDb, uid)
	if err != nil {
		slog.Error("Error counting talkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error counting talkgroups"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": total, "talkgroups": talkgroups})
}

func GETTalkgroup(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	idInt, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID"})
		return
	}
	if idInt < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID: negative value"})
		return
	}
	talkgroup, err := models.FindTalkgroupByID(db, uint(idInt))
	db.Preload("Admins").Preload("NCOs").Find(&talkgroup, "id = ?", id)
	if err != nil {
		slog.Error("Error finding talkgroup", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding talkgroup"})
		return
	}
	c.JSON(http.StatusOK, talkgroup)
}

func DELETETalkgroup(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	idUint64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID"})
		return
	}
	err = models.DeleteTalkgroup(db, uint(idUint64))
	if err != nil {
		slog.Error("Error deleting talkgroup", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting talkgroup"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Talkgroup deleted"})
}

func POSTTalkgroupNCOs(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	idInt, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID"})
		return
	}
	if idInt < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID: negative value"})
		return
	}

	talkgroup, err := models.FindTalkgroupByID(db, uint(idInt))
	if err != nil {
		slog.Error("Error finding talkgroup", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding talkgroup"})
		return
	}

	var json apimodels.TalkgroupAdminAction
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTTalkgroupNCOs", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		if len(json.UserIDs) == 0 {
			// remove all NCOs
			err := db.Model(&talkgroup).Association("NCOs").Clear()
			if err != nil {
				slog.Error("Error clearing NCOs", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error clearing NCOs"})
				return
			}
			err = db.Save(&talkgroup).Error
			if err != nil {
				slog.Error("Error saving talkgroup", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving talkgroup"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "Talkgroup admins cleared"})
			return
		}
		// add NCOs
		err := db.Model(&talkgroup).Association("NCOs").Clear()
		if err != nil {
			slog.Error("Error clearing NCOs", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error clearing NCOs"})
			return
		}
		for _, userID := range json.UserIDs {
			user, err := models.FindUserByID(db, userID)
			if err != nil {
				slog.Error("Error finding user", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding user"})
				return
			}
			err = db.Model(&talkgroup).Association("NCOs").Append(&user)
			if err != nil {
				slog.Error("Error appending NCO", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error appending NCO"})
				return
			}
		}
		err = db.Save(&talkgroup).Error
		if err != nil {
			slog.Error("Error saving talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving talkgroup"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "User appointed as net control operator"})
	}
}

func POSTTalkgroupAdmins(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	idInt, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID"})
		return
	}
	if idInt < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID: negative value"})
		return
	}

	talkgroup, err := models.FindTalkgroupByID(db, uint(idInt))
	if err != nil {
		slog.Error("Error finding talkgroup", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding talkgroup"})
		return
	}

	var json apimodels.TalkgroupAdminAction
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTTalkgroupAdmins", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		if len(json.UserIDs) == 0 {
			// remove all Admins
			err := db.Model(&talkgroup).Association("Admins").Clear()
			if err != nil {
				slog.Error("Error clearing talkgroup admins", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error clearing talkgroup admins"})
				return
			}
			err = db.Save(&talkgroup).Error
			if err != nil {
				slog.Error("Error saving talkgroup", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving talkgroup"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "Talkgroup admins cleared"})
			return
		}
		// add Admins
		err := db.Model(&talkgroup).Association("Admins").Clear()
		if err != nil {
			slog.Error("Error clearing talkgroup admins", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error clearing talkgroup admins"})
			return
		}
		for _, userID := range json.UserIDs {
			user, err := models.FindUserByID(db, userID)
			if err != nil {
				slog.Error("Error finding user", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding user"})
				return
			}
			err = db.Model(&talkgroup).Association("Admins").Append(&user)
			if err != nil {
				slog.Error("Error appending admin", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error appending admin"})
				return
			}
		}
		err = db.Save(&talkgroup).Error
		if err != nil {
			slog.Error("Error saving talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving talkgroup"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "User appointed as admin"})
	}
}

func PATCHTalkgroup(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	idInt, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID"})
		return
	}
	if idInt < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID: negative value"})
		return
	}
	var json apimodels.TalkgroupPatch
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "PATCHTalkgroup", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		talkgroup, err := models.FindTalkgroupByID(db, uint(idInt))
		if err != nil {
			slog.Error("Error finding talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding talkgroup"})
			return
		}

		if json.Name != "" {
			// Validate length less than 20 characters
			if len(json.Name) > maxNameLength {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Name must be less than 20 characters"})
				return
			}
			// Trim any whitespace
			json.Name = strings.TrimSpace(json.Name)
			// Check that length isn't 0
			if len(json.Name) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Name must be defined"})
				return
			}
			talkgroup.Name = json.Name
		}
		if json.Description != "" {
			// Validate length less than 240 characters
			if len(json.Description) > maxDescriptionLength {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Description must be less than 240 characters"})
				return
			}
			json.Description = strings.TrimSpace(json.Description)
			// Check that length isn't 0
			if len(json.Description) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Description must be defined"})
				return
			}
			talkgroup.Description = json.Description
		}

		err = db.Save(&talkgroup).Error
		if err != nil {
			slog.Error("Error saving talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving talkgroup"})
			return
		}
	}
}

func POSTTalkgroup(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	var json apimodels.TalkgroupPost
	err := c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTTalkgroup", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		if json.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
			return
		}
		// Validate length less than 20 characters
		if len(json.Name) > maxNameLength {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Name must be less than 20 characters"})
			return
		}
		if json.Description != "" {
			// Validate length less than 240 characters
			if len(json.Description) > maxDescriptionLength {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Description must be less than 240 characters"})
				return
			}
		}
		// Validate json.ID is not already in use
		exists, err := models.TalkgroupIDExists(db, json.ID)
		if err != nil {
			slog.Error("Error checking if talkgroup ID exists", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error checking if talkgroup ID exists"})
			return
		}
		if exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup ID already exists"})
			return
		}

		talkgroup := models.Talkgroup{
			ID:          json.ID,
			Name:        json.Name,
			Description: json.Description,
		}

		err = db.Create(&talkgroup).Error
		if err != nil {
			slog.Error("Error creating talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating talkgroup"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Talkgroup created"})
	}
}
