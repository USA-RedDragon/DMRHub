package talkgroups

import (
	"net/http"
	"strings"

	"github.com/USA-RedDragon/dmrserver-in-a-box/http/api/apimodels"
	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func GETTalkgroups(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	var talkgroups []models.Talkgroup
	db.Preload("Admins").Find(&talkgroups)
	c.JSON(http.StatusOK, talkgroups)
}

func GETMyTalkgroups(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	session := sessions.Default(c)

	userId := session.Get("user_id")
	if userId == nil {
		klog.Error("userId not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}

	var talkgroups []models.Talkgroup
	db.Table("talkgroup_admins")
	if err := db.Joins("JOIN talkgroup_admins on talkgroup_admins.talkgroup_id=talkgroups.id").
		Joins("JOIN users on talkgroup_admins.user_id=users.id").Where("users.id=?", userId.(uint)).
		Group("talkgroups.id").Find(&talkgroups).Error; err != nil {
		klog.Errorf("Error getting talkgroups owned by user %d: %v", userId.(uint), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting talkgroups owned by user"})
		return
	}

	c.JSON(http.StatusOK, talkgroups)
}

func GETTalkgroup(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
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
	c.JSON(http.StatusOK, talkgroup)
}

func DELETETalkgroup(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	db.Unscoped().Delete(&models.Talkgroup{}, "id = ?", c.Param("id"))
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Talkgroup deleted"})
}

func POSTTalkgroupAdminAppoint(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
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
		klog.Errorf("POSTTalkgroupAdminAppoint: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		alreadyHasAdmin := false
		for _, admin := range talkgroup.Admins {
			if admin.ID == json.UserID {
				alreadyHasAdmin = true
			}
		}
		if alreadyHasAdmin {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User is already an admin"})
			return
		} else {
			var user models.User
			db.Find(&user, "id = ?", json.UserID)
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
			if user.ID == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "User does not exist"})
				return
			}
			db.Model(&talkgroup).Association("Admins").Append(&user)
			db.Save(&talkgroup)
			if db.Error != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "User appointed as admin"})
		}
	}
}

func POSTTalkgroupAdminDemote(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
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
		klog.Errorf("POSTTalkgroupAdminDemote: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		removedAdmin := false
		for _, admin := range talkgroup.Admins {
			if admin.ID == json.UserID {
				db.Model(&talkgroup).Association("Admins").Delete(&admin)
				db.Save(&talkgroup)
				removedAdmin = true
			}
		}
		if removedAdmin {
			c.JSON(http.StatusOK, gin.H{"message": "Admin demoted"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User is not an admin"})
		}
	}
}

func PATCHTalkgroup(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
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
			if len(json.Name) > 20 {
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
			if len(json.Description) > 240 {
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
	db := c.MustGet("DB").(*gorm.DB)
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
		if len(json.Name) > 20 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Name must be less than 20 characters"})
			return
		}
		if json.Description != "" {
			// Validate length less than 240 characters
			if len(json.Description) > 240 {
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
