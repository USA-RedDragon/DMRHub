// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"net/http"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

const maxNameLength = 20
const maxDescriptionLength = 240

func GETTalkgroups(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	talkgroups := models.ListTalkgroups(db)
	total := models.CountTalkgroups(cDb)
	c.JSON(http.StatusOK, gin.H{"total": total, "talkgroups": talkgroups})
}

func GETMyTalkgroups(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	session := sessions.Default(c)

	userID := session.Get("user_id")
	if userID == nil {
		klog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		klog.Error("userID cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	talkgroups, err := models.FindTalkgroupsByOwnerID(db, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	total := models.CountTalkgroupsByOwnerID(cDb, uid)

	c.JSON(http.StatusOK, gin.H{"total": total, "talkgroups": talkgroups})
}

func GETTalkgroup(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	var talkgroup models.Talkgroup
	db.Preload("Admins").Preload("NCOs").Find(&talkgroup, "id = ?", id)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	if talkgroup.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup does not exist"})
		return
	}
	c.JSON(http.StatusOK, talkgroup)
}

func DELETETalkgroup(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	idUint64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID"})
		return
	}
	models.DeleteTalkgroup(db, uint(idUint64))
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Talkgroup deleted"})
}

func POSTTalkgroupNCOs(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	var talkgroup models.Talkgroup
	db.Preload("NCOs").Find(&talkgroup, "id = ?", id)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	if talkgroup.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup does not exist"})
		return
	}

	var json apimodels.TalkgroupAdminAction
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("POSTTalkgroupNCOs: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		if len(json.UserIDs) == 0 {
			// remove all NCOs
			err := db.Model(&talkgroup).Association("NCOs").Clear()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
			db.Save(&talkgroup)
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "Talkgroup admins cleared"})
			return
		}
		// add NCOs
		err := db.Model(&talkgroup).Association("NCOs").Clear()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, userID := range json.UserIDs {
			user := models.FindUserByID(db, userID)
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
			if user.ID == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "User does not exist"})
				return
			}
			err := db.Model(&talkgroup).Association("NCOs").Append(&user)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
		}
		db.Save(&talkgroup)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "User appointed as net control operator"})
	}
}

func POSTTalkgroupAdmins(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	var talkgroup models.Talkgroup
	db.Preload("Admins").Find(&talkgroup, "id = ?", id)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	if talkgroup.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup does not exist"})
		return
	}

	var json apimodels.TalkgroupAdminAction
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("POSTTalkgroupAdmins: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		if len(json.UserIDs) == 0 {
			// remove all Admins
			err := db.Model(&talkgroup).Association("Admins").Clear()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
			db.Save(&talkgroup)
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "Talkgroup admins cleared"})
			return
		}
		// add Admins
		err := db.Model(&talkgroup).Association("Admins").Clear()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, userID := range json.UserIDs {
			user := models.FindUserByID(db, userID)
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
			if user.ID == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "User does not exist"})
				return
			}
			err := db.Model(&talkgroup).Association("Admins").Append(&user)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
		}
		db.Save(&talkgroup)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "User appointed as admin"})
	}
}

func PATCHTalkgroup(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	var json apimodels.TalkgroupPatch
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("PATCHTalkgroup: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		var talkgroup models.Talkgroup
		db.Find(&talkgroup, "id = ?", id)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		if talkgroup.ID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup does not exist"})
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

		db.Save(&talkgroup)
	}
}

func POSTTalkgroup(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	var json apimodels.TalkgroupPost
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("POSTTalkgroup: JSON data is invalid: %v", err)
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
		var talkgroup models.Talkgroup
		db.Find(&talkgroup, "id = ?", json.ID)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		if talkgroup.ID != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup ID is already in use"})
			return
		}

		talkgroup.ID = json.ID
		talkgroup.Name = json.Name
		talkgroup.Description = json.Description
		db.Create(&talkgroup)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Talkgroup created"})
	}
}
