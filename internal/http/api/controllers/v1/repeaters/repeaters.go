package repeaters

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/models"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func GETRepeaters(c *gin.Context) {
	db := c.MustGet("PaginatedDB").(*gorm.DB)
	cDb := c.MustGet("DB").(*gorm.DB)
	repeaters := models.ListRepeaters(db)
	count := models.CountRepeaters(cDb)
	c.JSON(http.StatusOK, gin.H{"total": count, "repeaters": repeaters})
}

func GETMyRepeaters(c *gin.Context) {
	db := c.MustGet("PaginatedDB").(*gorm.DB)
	cDb := c.MustGet("DB").(*gorm.DB)
	session := sessions.Default(c)

	userId := session.Get("user_id")
	if userId == nil {
		klog.Error("userId not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}

	// Get all repeaters owned by user
	repeaters := models.GetUserRepeaters(db, userId.(uint))
	if db.Error != nil {
		klog.Errorf("Error getting repeaters owned by user %d: %v", userId, db.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters owned by user"})
		return
	}

	count := models.CountUserRepeaters(cDb, userId.(uint))

	c.JSON(http.StatusOK, gin.H{"total": count, "repeaters": repeaters})
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
	idUint64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup ID"})
		return
	}
	models.DeleteRepeater(db, uint(idUint64))
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Repeater deleted"})
}

func POSTRepeaterTalkgroups(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	redis := c.MustGet("Redis").(*redis.Client)
	id := c.Param("id")
	// Convert string id into uint
	rid, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Repeater ID"})
		return
	}
	repeaterID := uint(rid)

	var json apimodels.RepeaterTalkgroupsPost
	err = c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("POSTRepeaterTalkgroups: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
		return
	} else {
		if models.RepeaterIDExists(db, repeaterID) {
			repeater := models.FindRepeaterByID(db, repeaterID)
			db.Model(&repeater).Association("TS1StaticTalkgroups").Replace(json.TS1StaticTalkgroups)
			db.Model(&repeater).Association("TS2StaticTalkgroups").Replace(json.TS2StaticTalkgroups)

			if json.TS1DynamicTalkgroup.ID == 0 {
				repeater.TS1DynamicTalkgroupID = nil
				db.Model(&repeater).Association("TS1DynamicTalkgroup").Delete(&repeater.TS1DynamicTalkgroup)
			} else {
				repeater.TS1DynamicTalkgroupID = &json.TS1DynamicTalkgroup.ID
				repeater.TS1DynamicTalkgroup = json.TS1DynamicTalkgroup
				db.Model(&repeater).Association("TS1DynamicTalkgroup").Replace(&json.TS1DynamicTalkgroup)
			}

			if json.TS2DynamicTalkgroup.ID == 0 {
				repeater.TS2DynamicTalkgroupID = nil
				db.Model(&repeater).Association("TS2DynamicTalkgroup").Delete(&repeater.TS2DynamicTalkgroup)
			} else {
				repeater.TS2DynamicTalkgroupID = &json.TS2DynamicTalkgroup.ID
				repeater.TS2DynamicTalkgroup = json.TS2DynamicTalkgroup
				db.Model(&repeater).Association("TS2DynamicTalkgroup").Replace(&json.TS2DynamicTalkgroup)
			}

			db.Save(&repeater)
			repeater.CancelAllSubscriptions()
			go repeater.ListenForCalls(c.Request.Context(), redis)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
			return
		}
	}
}

func POSTRepeater(c *gin.Context) {
	session := sessions.Default(c)
	usId := session.Get("user_id")
	if usId == nil {
		klog.Error("userId not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
	userId := usId.(uint)
	db := c.MustGet("DB").(*gorm.DB)
	redis := c.MustGet("Redis").(*redis.Client)

	var user models.User
	db.First(&user, userId)
	if db.Error != nil {
		klog.Errorf("Error getting user %d: %v", userId, db.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting user"})
		return
	}

	var json apimodels.RepeaterPost
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("POSTRepeater: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		var repeater models.Repeater

		// if json.RadioID is a hotspot, then it will be 7 or 9 digits long and be prefixed by the userId
		hotspotRegex := regexp.MustCompile(`^` + fmt.Sprintf("%d", userId) + `([0][1-9]|[1-9][0-9])?$`)
		// if json.RadioID is a repeater, then it must be 6 digits long
		repeaterRegex := regexp.MustCompile(`^[0-9]{6}$`)

		if hotspotRegex.MatchString(fmt.Sprintf("%d", json.RadioID)) {
			repeater.Hotspot = true
		} else if repeaterRegex.MatchString(fmt.Sprintf("%d", json.RadioID)) {
			repeater.Hotspot = false
			if !repeaterdb.IsValidRepeaterID(json.RadioID) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater ID is not valid"})
				return
			}
			if !repeaterdb.IsInDB(json.RadioID, user.Callsign) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater ID does not match assigned callsign"})
				return
			}
			r, err := repeaterdb.GetRepeater(json.RadioID)
			if err != nil {
				klog.Errorf("Error getting repeater from database: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeater from database"})
				return
			}
			repeater.Callsign = r.Callsign
			repeater.ColorCode = r.ColorCode
			// Location is a string with r.City, r.State, and r.Country, set repeater.Location
			repeater.Location = r.City + ", " + r.State + ", " + r.Country
			repeater.Description = r.MapInfo
			// r.Frequency is a string in MHz with a decimal, convert to an int in Hz and set repeater.RXFrequency
			mhZFloat, err := strconv.ParseFloat(r.Frequency, 32)
			if err != nil {
				klog.Errorf("Error converting frequency to float: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error converting frequency to float"})
				return
			}
			repeater.TXFrequency = uint(mhZFloat * 1000000)
			// r.Offset is a string with +/- and a decimal in MHz, convert to an int in Hz and set repeater.TXFrequency to RXFrequency +/- Offset
			positiveOffset := false
			if strings.HasPrefix(r.Offset, "-") {
				positiveOffset = false
			} else {
				positiveOffset = true
			}
			// strip the +/- from the offset
			r.Offset = strings.TrimPrefix(r.Offset, "-")
			r.Offset = strings.TrimPrefix(r.Offset, "+")
			// convert the offset to a float
			offsetFloat, err := strconv.ParseFloat(r.Offset, 32)
			if err != nil {
				klog.Errorf("Error converting offset to float: %v\nError:", r.Offset, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error converting offset to float"})
				return
			}
			// convert the offset to an int in Hz
			offsetInt := uint(offsetFloat * 1000000)
			if positiveOffset {
				repeater.RXFrequency = repeater.TXFrequency + offsetInt
			} else {
				repeater.RXFrequency = repeater.TXFrequency - offsetInt
			}
			// TODO: maybe handle TSLinked?
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "RadioID is invalid"})
			return
		}

		repeater.RadioID = json.RadioID

		// Generate a random password of 8 characters
		repeater.Password = utils.RandomPassword(8, 1, 2)

		// Find user by userId
		repeater.Owner = user
		repeater.OwnerID = user.ID
		db.Preload("Owner").Create(&repeater)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		go repeater.ListenForCalls(c.Request.Context(), redis)
		c.JSON(http.StatusOK, gin.H{"message": "Repeater created", "password": repeater.Password})
	}
}

func POSTRepeaterLink(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	redis := c.MustGet("Redis").(*redis.Client)
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
			repeater.TS1DynamicTalkgroupID = &talkgroup.ID
		case "2":
			// Set TS2DynamicTalkgroup association on repeater to target
			repeater.TS2DynamicTalkgroup = talkgroup
			repeater.TS2DynamicTalkgroupID = &talkgroup.ID
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
	go repeater.ListenForCallsOn(c.Request.Context(), redis, talkgroup.ID)
	db.Save(&repeater)
}

func POSTRepeaterUnlink(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	linkType := c.Param("type")
	slot := c.Param("slot")
	target := c.Param("target")

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
	targetUint64, err := strconv.ParseUint(target, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Talkgroup ID"})
		return
	}
	targetUint := uint(targetUint64)
	if !models.TalkgroupIDExists(db, targetUint) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup does not exist"})
		return
	}
	talkgroup := models.FindTalkgroupByID(db, targetUint)

	// Convert id to a uint
	idUint64, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Repeater ID"})
		return
	}
	idUint := uint(idUint64)

	if !models.RepeaterIDExists(db, idUint) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
		return
	}

	repeater := models.FindRepeaterByID(db, idUint)

	switch linkType {
	case "dynamic":
		switch slot {
		case "1":
			if repeater.TS1DynamicTalkgroupID == nil || *repeater.TS1DynamicTalkgroupID != talkgroup.ID {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup is not linked to repeater"})
				return
			}
			oldTGID := *repeater.TS1DynamicTalkgroupID
			// Set TS1DynamicTalkgroup association on repeater to target
			repeater.TS1DynamicTalkgroup = models.Talkgroup{}
			repeater.TS1DynamicTalkgroupID = nil

			repeater.CancelSubscription(oldTGID)

			db.Save(&repeater)
		case "2":
			if repeater.TS2DynamicTalkgroupID == nil || *repeater.TS2DynamicTalkgroupID != talkgroup.ID {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup is not linked to repeater"})
				return
			}
			oldTGID := *repeater.TS2DynamicTalkgroupID
			// Set TS2DynamicTalkgroup association on repeater to target
			repeater.TS2DynamicTalkgroup = models.Talkgroup{}
			repeater.TS2DynamicTalkgroupID = nil

			repeater.CancelSubscription(oldTGID)

			db.Save(&repeater)
		}
	case "static":
		switch slot {
		case "1":
			// Look in TS1StaticTalkgroups for the target
			// If found, remove it
			var found bool
			for _, tg := range repeater.TS1StaticTalkgroups {
				if tg.ID == talkgroup.ID {
					oldID := talkgroup.ID
					db.Model(&repeater).Association("TS1StaticTalkgroups").Delete(&talkgroup)
					repeater.CancelSubscription(oldID)
					db.Save(&repeater)
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
					oldID := talkgroup.ID
					db.Model(&repeater).Association("TS2StaticTalkgroups").Delete(&talkgroup)
					repeater.CancelSubscription(oldID)
					db.Save(&repeater)
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
	c.JSON(http.StatusOK, gin.H{"message": "Timeslot unlinked"})
}
