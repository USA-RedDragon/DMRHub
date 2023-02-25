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

package repeaters

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/hbrp"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	LinkTypeDynamic = "dynamic"
	LinkTypeStatic  = "static"
)

func GETRepeaters(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	repeaters, err := models.ListRepeaters(db)
	if err != nil {
		logging.Errorf("Error getting repeaters: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters"})
		return
	}

	count, err := models.CountRepeaters(cDb)
	if err != nil {
		logging.Errorf("Error getting repeaters: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": count, "repeaters": repeaters})
}

func GETMyRepeaters(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	session := sessions.Default(c)

	userID := session.Get("user_id")
	if userID == nil {
		logging.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		logging.Errorf("Unable to convert userID to uint: %v", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Get all repeaters owned by user
	repeaters, err := models.GetUserRepeaters(db, uid)
	if err != nil {
		logging.Errorf("Error getting repeaters owned by user %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters owned by user"})
		return
	}

	count, err := models.CountUserRepeaters(cDb, uid)
	if err != nil {
		logging.Errorf("Error getting repeaters owned by user %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters owned by user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": count, "repeaters": repeaters})
}

func GETRepeater(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	// Convert string id into uint
	repeaterID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Repeater ID"})
		return
	}
	repeaterExists, err := models.RepeaterIDExists(db, uint(repeaterID))
	if err != nil {
		logging.Errorf("Error checking if repeater exists: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error checking if repeater exists"})
		return
	}

	if !repeaterExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
		return
	}

	repeater, err := models.FindRepeaterByID(db, uint(repeaterID))
	if err != nil {
		logging.Errorf("Error getting repeater: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeater"})
		return
	}

	c.JSON(http.StatusOK, repeater)
}

func DELETERepeater(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	idUint64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid repeater ID"})
		return
	}
	err = models.DeleteRepeater(db, uint(idUint64))
	if err != nil {
		logging.Errorf("Error deleting repeater: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting repeater"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Repeater deleted"})
}

func POSTRepeaterTalkgroups(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	redis, ok := c.MustGet("Redis").(*redis.Client)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
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
		logging.Errorf("POSTRepeaterTalkgroups: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
		return
	}
	repeaterExists, err := models.RepeaterIDExists(db, repeaterID)
	if err != nil {
		logging.Errorf("POSTRepeaterTalkgroups: Error checking if repeater exists: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error checking if repeater exists"})
		return
	}

	if !repeaterExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
		return
	}

	repeater, err := models.FindRepeaterByID(db, repeaterID)
	if err != nil {
		logging.Errorf("POSTRepeaterTalkgroups: Error getting repeater: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeater"})
		return
	}

	err = db.Model(&repeater).Association("TS1StaticTalkgroups").Replace(json.TS1StaticTalkgroups)
	if err != nil {
		logging.Errorf("POSTRepeaterTalkgroups: Error updating TS1StaticTalkgroups: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating TS1StaticTalkgroups"})
		return
	}
	repeater.TS1StaticTalkgroups = json.TS1StaticTalkgroups
	err = db.Model(&repeater).Association("TS2StaticTalkgroups").Replace(json.TS2StaticTalkgroups)
	if err != nil {
		logging.Errorf("POSTRepeaterTalkgroups: Error updating TS2StaticTalkgroups: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating TS2StaticTalkgroups"})
		return
	}
	repeater.TS2StaticTalkgroups = json.TS2StaticTalkgroups

	if json.TS1DynamicTalkgroup.ID == 0 {
		repeater.TS1DynamicTalkgroupID = nil
		err = db.Model(&repeater).Association("TS1DynamicTalkgroup").Delete(&repeater.TS1DynamicTalkgroup)
		if err != nil {
			logging.Errorf("POSTRepeaterTalkgroups: Error deleting TS1DynamicTalkgroup: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting TS1DynamicTalkgroup"})
			return
		}
	} else {
		repeater.TS1DynamicTalkgroupID = &json.TS1DynamicTalkgroup.ID
		repeater.TS1DynamicTalkgroup = json.TS1DynamicTalkgroup
		err = db.Model(&repeater).Association("TS1DynamicTalkgroup").Replace(&json.TS1DynamicTalkgroup)
		if err != nil {
			logging.Errorf("POSTRepeaterTalkgroups: Error updating TS1DynamicTalkgroup: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating TS1DynamicTalkgroup"})
			return
		}
	}

	if json.TS2DynamicTalkgroup.ID == 0 {
		repeater.TS2DynamicTalkgroupID = nil
		err = db.Model(&repeater).Association("TS2DynamicTalkgroup").Delete(&repeater.TS2DynamicTalkgroup)
		if err != nil {
			logging.Errorf("POSTRepeaterTalkgroups: Error deleting TS2DynamicTalkgroup: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting TS2DynamicTalkgroup"})
			return
		}
	} else {
		repeater.TS2DynamicTalkgroupID = &json.TS2DynamicTalkgroup.ID
		repeater.TS2DynamicTalkgroup = json.TS2DynamicTalkgroup
		err = db.Model(&repeater).Association("TS2DynamicTalkgroup").Replace(&json.TS2DynamicTalkgroup)
		if err != nil {
			logging.Errorf("POSTRepeaterTalkgroups: Error updating TS2DynamicTalkgroup: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating TS2DynamicTalkgroup"})
			return
		}
	}

	err = db.Save(&repeater).Error
	if err != nil {
		logging.Errorf("POSTRepeaterTalkgroups: Error saving repeater: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
		return
	}
	hbrp.GetSubscriptionManager().CancelAllRepeaterSubscriptions(repeater)
	go hbrp.GetSubscriptionManager().ListenForCalls(redis, repeater)
	c.JSON(http.StatusOK, gin.H{"message": "Repeater talkgroups updated"})
}

func POSTRepeater(c *gin.Context) {
	session := sessions.Default(c)
	usID := session.Get("user_id")
	if usID == nil {
		logging.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
	userID, ok := usID.(uint)
	if !ok {
		logging.Error("userID cast failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	redis, ok := c.MustGet("Redis").(*redis.Client)
	if !ok {
		logging.Error("Redis cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	user, err := models.FindUserByID(db, userID)
	if err != nil {
		logging.Errorf("Error getting user %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting user"})
		return
	}

	var json apimodels.RepeaterPost
	err = c.ShouldBindJSON(&json)
	if err != nil {
		logging.Errorf("POSTRepeater: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		var repeater models.Repeater

		// if json.RadioID is a hotspot, then it will be 7 or 9 digits long and be prefixed by the userID
		hotspotRegex := regexp.MustCompile(`^` + fmt.Sprintf("%d", userID) + `([0][1-9]|[1-9][0-9])?$`)
		// if json.RadioID is a repeater, then it must be 6 digits long
		repeaterRegex := regexp.MustCompile(`^[0-9]{6}$`)

		switch {
		case repeaterRegex.MatchString(fmt.Sprintf("%d", json.RadioID)):
			repeater.Hotspot = false
			if !repeaterdb.IsValidRepeaterID(json.RadioID) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater ID is not valid"})
				return
			}
			if !repeaterdb.ValidRepeaterCallsign(json.RadioID, user.Callsign) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater ID does not match assigned callsign"})
				return
			}
			r, ok := repeaterdb.Get(json.RadioID)
			if !ok {
				logging.Error("Error getting repeater from database")
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeater from database"})
				return
			}
			repeater.Callsign = r.Callsign
			repeater.ColorCode = r.ColorCode
			// Location is a string with r.City, r.State, and r.Country, set repeater.Location
			repeater.Location = r.City + ", " + r.State + ", " + r.Country
			repeater.Description = r.MapInfo
			// r.Frequency is a string in MHz with a decimal, convert to an int in Hz and set repeater.RXFrequency
			mhZFloat, parseErr := strconv.ParseFloat(r.Frequency, 32)
			if parseErr != nil {
				logging.Errorf("Error converting frequency to float: %v", parseErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error converting frequency to float"})
				return
			}
			const mHzToHz = 1000000
			repeater.TXFrequency = uint(mhZFloat * mHzToHz)
			// r.Offset is a string with +/- and a decimal in MHz, convert to an int in Hz and set repeater.TXFrequency to RXFrequency +/- Offset
			var positiveOffset bool
			if strings.HasPrefix(r.Offset, "-") {
				positiveOffset = false
			} else {
				positiveOffset = true
			}
			// strip the +/- from the offset
			r.Offset = strings.TrimPrefix(r.Offset, "-")
			r.Offset = strings.TrimPrefix(r.Offset, "+")
			// convert the offset to a float
			offsetFloat, parseErr := strconv.ParseFloat(r.Offset, 32)
			if parseErr != nil {
				logging.Errorf("Error converting offset to float: %v\nError: %v", r.Offset, parseErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error converting offset to float"})
				return
			}
			// convert the offset to an int in Hz
			offsetInt := uint(offsetFloat * mHzToHz)
			if positiveOffset {
				repeater.RXFrequency = repeater.TXFrequency + offsetInt
			} else {
				repeater.RXFrequency = repeater.TXFrequency - offsetInt
			}
		case hotspotRegex.MatchString(fmt.Sprintf("%d", json.RadioID)):
			repeater.Hotspot = true
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "RadioID is invalid"})
			return
		}

		repeater.RadioID = json.RadioID

		// Generate a random password of 8 characters
		const randLen = 8
		const randNum = 1
		const randSpecial = 2
		repeater.Password, err = utils.RandomPassword(randLen, randNum, randSpecial)
		if err != nil {
			logging.Errorf("Failed to generate a repeater password %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to generate a repeater password"})
			return
		}

		// Find user by userID
		repeater.Owner = user
		repeater.OwnerID = user.ID
		err := db.Preload("Owner").Create(&repeater).Error
		if err != nil {
			logging.Errorf("Error creating repeater: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating repeater"})
			return
		}
		go hbrp.GetSubscriptionManager().ListenForCalls(redis, repeater)
		c.JSON(http.StatusOK, gin.H{"message": "Repeater created", "password": repeater.Password})
	}
}

func POSTRepeaterLink(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	redis, ok := c.MustGet("Redis").(*redis.Client)
	if !ok {
		logging.Error("Redis cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	linkType := c.Param("type")
	slot := c.Param("slot")
	target := c.Param("target")
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid repeater ID"})
		return
	}

	repeater, err := models.FindRepeaterByID(db, uint(id))
	if err != nil {
		logging.Errorf("Error finding repeater: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding repeater"})
		return
	}
	// LinkType should be either "dynamic" or "static"
	if linkType != LinkTypeDynamic && linkType != LinkTypeStatic {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link type"})
		return
	}
	// Slot should be either "1" or "2"
	if slot != "1" && slot != "2" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid slot"})
		return
	}

	targetInt, err := strconv.Atoi(target)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid target"})
		return
	}
	// Validate target is a valid talkgroup
	exists, err := models.TalkgroupIDExists(db, uint(targetInt))
	if err != nil {
		logging.Errorf("Error validating target: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error validating target"})
		return
	}
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid target"})
		return
	}

	talkgroup, err := models.FindTalkgroupByID(db, uint(targetInt))
	if err != nil {
		logging.Errorf("Error finding talkgroup: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding talkgroup"})
		return
	}

	switch linkType {
	case LinkTypeDynamic:
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
	case LinkTypeStatic:
		switch slot {
		case "1":
			// Append TS1StaticTalkgroups association on repeater to target
			err := db.Model(&repeater).Association("TS1StaticTalkgroups").Append(&talkgroup)
			if err != nil {
				logging.Errorf("Error appending TS1StaticTalkgroups: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error appending TS1StaticTalkgroups"})
				return
			}
		case "2":
			// Append TS2StaticTalkgroups association on repeater to target
			err := db.Model(&repeater).Association("TS2StaticTalkgroups").Append(&talkgroup)
			if err != nil {
				logging.Errorf("Error appending TS2StaticTalkgroups: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error appending TS2StaticTalkgroups"})
				return
			}
		}
	}
	go hbrp.GetSubscriptionManager().ListenForCallsOn(redis, repeater, talkgroup.ID)
	err = db.Save(&repeater).Error
	if err != nil {
		logging.Errorf("Error saving repeater: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
		return
	}
}

//nolint:golint,gocyclo
func POSTRepeaterUnlink(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	linkType := c.Param("type")
	slot := c.Param("slot")
	target := c.Param("target")

	// LinkType should be either "dynamic" or "static"
	if linkType != LinkTypeDynamic && linkType != LinkTypeStatic {
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
	talkgroupExists, err := models.TalkgroupIDExists(db, targetUint)
	if err != nil {
		logging.Errorf("Error validating target: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error validating target"})
		return
	}

	if !talkgroupExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid target"})
		return
	}

	talkgroup, err := models.FindTalkgroupByID(db, targetUint)
	if err != nil {
		logging.Errorf("Error finding talkgroup: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding talkgroup"})
		return
	}

	// Convert id to a uint
	idUint64, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Repeater ID"})
		return
	}
	idUint := uint(idUint64)

	repeaterExists, err := models.RepeaterIDExists(db, idUint)
	if err != nil {
		logging.Errorf("Error validating repeater: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error validating repeater"})
		return
	}

	if !repeaterExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater does not exist"})
		return
	}

	repeater, err := models.FindRepeaterByID(db, idUint)
	if err != nil {
		logging.Errorf("Error finding repeater: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding repeater"})
		return
	}

	switch linkType {
	case LinkTypeDynamic:
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

			hbrp.GetSubscriptionManager().CancelSubscription(repeater, oldTGID)

			err := db.Save(&repeater).Error
			if err != nil {
				logging.Errorf("Error saving repeater: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
				return
			}
		case "2":
			if repeater.TS2DynamicTalkgroupID == nil || *repeater.TS2DynamicTalkgroupID != talkgroup.ID {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Talkgroup is not linked to repeater"})
				return
			}
			oldTGID := *repeater.TS2DynamicTalkgroupID
			// Set TS2DynamicTalkgroup association on repeater to target
			repeater.TS2DynamicTalkgroup = models.Talkgroup{}
			repeater.TS2DynamicTalkgroupID = nil

			hbrp.GetSubscriptionManager().CancelSubscription(repeater, oldTGID)

			err := db.Save(&repeater).Error
			if err != nil {
				logging.Errorf("Error saving repeater: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
				return
			}
		}
	case LinkTypeStatic:
		switch slot {
		case "1":
			// Look in TS1StaticTalkgroups for the target
			// If found, remove it
			var found bool
			for _, tg := range repeater.TS1StaticTalkgroups {
				if tg.ID == talkgroup.ID {
					oldID := talkgroup.ID
					err := db.Model(&repeater).Association("TS1StaticTalkgroups").Delete(&talkgroup)
					if err != nil {
						logging.Errorf("Error deleting TS1StaticTalkgroups: %v", err)
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting TS1StaticTalkgroups"})
						return
					}
					hbrp.GetSubscriptionManager().CancelSubscription(repeater, oldID)
					err = db.Save(&repeater).Error
					if err != nil {
						logging.Errorf("Error saving repeater: %v", err)
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
						return
					}
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
					err := db.Model(&repeater).Association("TS2StaticTalkgroups").Delete(&talkgroup)
					if err != nil {
						logging.Errorf("Error deleting TS2StaticTalkgroups: %v", err)
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting TS2StaticTalkgroups"})
						return
					}
					hbrp.GetSubscriptionManager().CancelSubscription(repeater, oldID)
					err = db.Save(&repeater).Error
					if err != nil {
						logging.Errorf("Error saving repeater: %v", err)
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
						return
					}
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
