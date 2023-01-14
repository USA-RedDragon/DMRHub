package repeaters

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/USA-RedDragon/dmrserver-in-a-box/http/api/apimodels"
	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func GETRepeaters(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	repeaters := models.ListRepeaters(db)
	c.JSON(http.StatusOK, repeaters)
}

func GETMyRepeaters(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	session := sessions.Default(c)

	userId := session.Get("user_id").(uint)
	if userId == 0 {
		klog.Error("userId not found")
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Authentication failed"})
		return
	}

	user := models.FindUserByID(db, userId)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	if user.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User does not exist"})
		return
	}
	c.JSON(http.StatusOK, user.Repeaters)
}

func GETRepeater(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	// Convert string id into uint
	repeaterID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Repeater ID"})
		return
	}
	if models.RepeaterIDExists(db, uint(repeaterID)) {
		repeater := models.FindRepeaterByID(db, uint(repeaterID))
		c.JSON(http.StatusOK, repeater)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
	}
}

func DELETERepeater(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	db.Unscoped().Delete(&models.Repeater{}, "radio_id = ?", id)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Repeater deleted"})
}

func POSTRepeater(c *gin.Context) {
	session := sessions.Default(c)
	usId := session.Get("user_id")
	if usId == nil {
		klog.Error("userId not found")
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Authentication failed"})
	}
	userId := usId.(uint)
	db := c.MustGet("DB").(*gorm.DB)
	var json apimodels.RepeaterPost
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("POSTRepeater: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		var repeater models.Repeater

		// Validate repeater.RadioID matches the userId or the userId suffixed by a two-digit number between 01 and 10
		re := regexp.MustCompile(`^` + fmt.Sprintf("%d", userId) + `([0][1-9]|[1-9][0-9])?$`)
		if !re.MatchString(fmt.Sprintf("%d", json.RadioID)) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "RadioID does not match the user id"})
			return
		}

		repeater.RadioID = json.RadioID

		// Validate password is at least 5 characters
		if len(json.Password) < 5 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 5 characters"})
			return
		}

		repeater.Password = json.Password
		// Find user by userId
		var user models.User
		db.Find(&user, "id = ?", userId)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		if user.ID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User does not exist"})
			return
		}
		repeater.Owner = user
		repeater.OwnerID = user.ID
		db.Create(&repeater)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Repeater created"})
	}
}

func PATCHRepeater(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	var json apimodels.RepeaterPatch
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("PATCHRepeater: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		var repeater models.Repeater
		db.Find(&repeater, "radio_id = ?", id)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		if repeater.RadioID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
			return
		}
		// Validate password is at least 5 characters
		if len(json.Password) < 5 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 5 characters"})
			return
		}
		repeater.Password = json.Password

		db.Save(&repeater)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Repeater updated"})
	}
}

func POSTRepeaterLink(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	linkType := c.Param("type")
	slot := c.Param("slot")
	target := c.Param("target")
	var repeater models.Repeater
	db.Find(&repeater, "radio_id = ?", id)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	if repeater.RadioID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
		return
	}
	// LinkType should be either "dynamic" or "static"
	if linkType != "dynamic" && linkType != "static" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link type"})
		return
	}
	// Slot should be either "1" or "2"
	if slot != "1" && slot != "2" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid slot"})
		return
	}
	// Validate target is a valid talkgroup
	var talkgroup models.Talkgroup
	db.Find(&talkgroup, "talkgroup_id = ?", target)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	switch linkType {
	case "dynamic":
		switch slot {
		case "1":
			// Set TS1DynamicTalkgroup association on repeater to target
			repeater.TS1DynamicTalkgroup = talkgroup
			repeater.TS1DynamicTalkgroupID = talkgroup.ID
		case "2":
			// Set TS2DynamicTalkgroup association on repeater to target
			repeater.TS2DynamicTalkgroup = talkgroup
			repeater.TS2DynamicTalkgroupID = talkgroup.ID
		}
	case "static":
		switch slot {
		case "1":
			// Append TS1StaticTalkgroups association on repeater to target
			db.Model(&repeater).Association("TS1StaticTalkgroups").Append(&talkgroup)
		case "2":
			// Append TS2StaticTalkgroups association on repeater to target
			db.Model(&repeater).Association("TS2StaticTalkgroups").Append(&talkgroup)
		}
	}
}

func POSTRepeaterUnink(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	linkType := c.Param("type")
	slot := c.Param("slot")
	target := c.Param("target")
	var repeater models.Repeater
	db.Find(&repeater, "radio_id = ?", id)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	if repeater.RadioID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
		return
	}
	// LinkType should be either "dynamic" or "static"
	if linkType != "dynamic" && linkType != "static" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link type"})
		return
	}
	// Slot should be either "1" or "2"
	if slot != "1" && slot != "2" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid slot"})
		return
	}
	// Validate target is a valid talkgroup
	var talkgroup models.Talkgroup
	db.Find(&talkgroup, "talkgroup_id = ?", target)
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	switch linkType {
	case "dynamic":
		switch slot {
		case "1":
			if repeater.TS1DynamicTalkgroupID != talkgroup.ID {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup is not linked to repeater"})
				return
			}
			// Set TS1DynamicTalkgroup association on repeater to target
			repeater.TS1DynamicTalkgroup = models.Talkgroup{}
			repeater.TS1DynamicTalkgroupID = 0
		case "2":
			if repeater.TS2DynamicTalkgroupID != talkgroup.ID {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup is not linked to repeater"})
				return
			}
			// Set TS2DynamicTalkgroup association on repeater to target
			repeater.TS2DynamicTalkgroup = models.Talkgroup{}
			repeater.TS2DynamicTalkgroupID = 0
		}
	case "static":
		switch slot {
		case "1":
			// Look in TS1StaticTalkgroups for the target
			// If found, remove it
			var found bool
			for _, tg := range repeater.TS1StaticTalkgroups {
				if tg.ID == talkgroup.ID {
					db.Model(&repeater).Association("TS1StaticTalkgroups").Delete(&talkgroup)
					found = true
					break
				}
			}
			if !found {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup is not linked to repeater"})
				return
			}

		case "2":
			// Look in TS2StaticTalkgroups for the target
			// If found, remove it
			var found bool
			for _, tg := range repeater.TS2StaticTalkgroups {
				if tg.ID == talkgroup.ID {
					db.Model(&repeater).Association("TS2StaticTalkgroups").Delete(&talkgroup)
					found = true
					break
				}
			}
			if !found {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup is not linked to repeater"})
				return
			}
		}
	}
}
