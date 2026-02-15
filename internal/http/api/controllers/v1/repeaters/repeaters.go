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
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	LinkTypeDynamic = "dynamic"
	LinkTypeStatic  = "static"
)

// repeaterIDRegex validates that a radio ID is a 6-digit repeater ID.
// Compiled once at package level to avoid re-compilation on every request.
var repeaterIDRegex = regexp.MustCompile(`^[0-9]{6}$`)

func GETRepeaters(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	repeaters, err := models.ListRepeaters(db)
	if err != nil {
		slog.Error("Error getting repeaters", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters"})
		return
	}

	count, err := models.CountRepeaters(cDb)
	if err != nil {
		slog.Error("Error getting repeaters", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": count, "repeaters": repeaters})
}

func GETMyRepeaters(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
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
		slog.Error("Unable to convert userID to uint", "function", "GETMyRepeaters", "userID", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Get all repeaters owned by user
	repeaters, err := models.GetUserRepeaters(db, uid)
	if err != nil {
		slog.Error("Error getting repeaters owned by user", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters owned by user"})
		return
	}

	count, err := models.CountUserRepeaters(cDb, uid)
	if err != nil {
		slog.Error("Error getting repeaters owned by user", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeaters owned by user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": count, "repeaters": repeaters})
}

func GETRepeater(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate and fetch repeater
	repeater, err := validateAndFetchRepeater(db, repeaterID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			slog.Error("Error validating repeater", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeater"})
		}
		return
	}

	c.JSON(http.StatusOK, *repeater)
}

func DELETERepeater(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = models.DeleteRepeater(db, repeaterID)
	if err != nil {
		slog.Error("Error deleting repeater", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting repeater"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Repeater deleted"})
}

func POSTRepeaterTalkgroups(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var json apimodels.RepeaterTalkgroupsPost
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
		return
	}

	// Validate and fetch repeater
	repeater, err := validateAndFetchRepeater(db, repeaterID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			slog.Error("Error validating repeater", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeater"})
		}
		return
	}

	err = db.Model(repeater).Association("TS1StaticTalkgroups").Replace(json.TS1StaticTalkgroups)
	if err != nil {
		slog.Error("Error updating TS1StaticTalkgroups", "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating TS1StaticTalkgroups"})
		return
	}
	repeater.TS1StaticTalkgroups = json.TS1StaticTalkgroups
	err = db.Model(repeater).Association("TS2StaticTalkgroups").Replace(json.TS2StaticTalkgroups)
	if err != nil {
		slog.Error("Error updating TS2StaticTalkgroups", "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating TS2StaticTalkgroups"})
		return
	}
	repeater.TS2StaticTalkgroups = json.TS2StaticTalkgroups

	if json.TS1DynamicTalkgroup.ID == 0 {
		repeater.TS1DynamicTalkgroupID = nil
		err = db.Model(repeater).Association("TS1DynamicTalkgroup").Delete(&repeater.TS1DynamicTalkgroup)
		if err != nil {
			slog.Error("Error deleting TS1DynamicTalkgroup", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting TS1DynamicTalkgroup"})
			return
		}
	} else {
		repeater.TS1DynamicTalkgroupID = &json.TS1DynamicTalkgroup.ID
		repeater.TS1DynamicTalkgroup = json.TS1DynamicTalkgroup
		err = db.Model(repeater).Association("TS1DynamicTalkgroup").Replace(&json.TS1DynamicTalkgroup)
		if err != nil {
			slog.Error("Error updating TS1DynamicTalkgroup", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating TS1DynamicTalkgroup"})
			return
		}
	}

	if json.TS2DynamicTalkgroup.ID == 0 {
		repeater.TS2DynamicTalkgroupID = nil
		err = db.Model(repeater).Association("TS2DynamicTalkgroup").Delete(&repeater.TS2DynamicTalkgroup)
		if err != nil {
			slog.Error("Error deleting TS2DynamicTalkgroup", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting TS2DynamicTalkgroup"})
			return
		}
	} else {
		repeater.TS2DynamicTalkgroupID = &json.TS2DynamicTalkgroup.ID
		repeater.TS2DynamicTalkgroup = json.TS2DynamicTalkgroup
		err = db.Model(repeater).Association("TS2DynamicTalkgroup").Replace(&json.TS2DynamicTalkgroup)
		if err != nil {
			slog.Error("Error updating TS2DynamicTalkgroup", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating TS2DynamicTalkgroup"})
			return
		}
	}

	err = db.Save(repeater).Error
	if err != nil {
		slog.Error("Error saving repeater", "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
		return
	}
	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if !ok {
		slog.Error("Hub cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	go dmrHub.ReloadRepeater(context.Background(), repeater.ID)
	c.JSON(http.StatusOK, gin.H{"message": "Repeater talkgroups updated"})
}

func POSTRepeater(c *gin.Context) {
	session := sessions.Default(c)
	usID := session.Get("user_id")
	if usID == nil {
		slog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}
	userID, ok := usID.(uint)
	if !ok {
		slog.Error("userID cast failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	config, ok := c.MustGet("Config").(*config.Config)
	if !ok {
		slog.Error("Config cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	user, err := models.FindUserByID(db, userID)
	if err != nil {
		slog.Error("Error getting user", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting user"})
		return
	}

	var json apimodels.RepeaterPost
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTRepeater", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		// Default type to MMDVM if not specified
		if json.Type == "" {
			json.Type = models.RepeaterTypeMMDVM
		}
		if json.Type != models.RepeaterTypeMMDVM && json.Type != models.RepeaterTypeIPSC {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid repeater type"})
			return
		}

		var repeater models.Repeater
		repeater.Type = json.Type
		repeater.SimplexRepeater = json.SimplexRepeater

		if json.Type == models.RepeaterTypeIPSC {
			// IPSC repeaters: validate RadioID as 6-digit or hotspot
			hotspotRegex := regexp.MustCompile(`^` + fmt.Sprintf("%d", userID) + `([0][1-9]|[1-9][0-9])?$`)

			switch {
			case repeaterIDRegex.MatchString(fmt.Sprintf("%d", json.RadioID)):
				repeater.Hotspot = false
			case hotspotRegex.MatchString(fmt.Sprintf("%d", json.RadioID)):
				repeater.Hotspot = true
			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": "RadioID is invalid"})
				return
			}

			repeater.ID = json.RadioID

			// Generate a random hex auth key (20 bytes = 40 hex chars) for IPSC HMAC-SHA1
			const ipscKeyLen = 40
			repeater.Password, err = utils.RandomHexString(ipscKeyLen)
			if err != nil {
				slog.Error("Failed to generate an IPSC auth key", "error", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to generate an IPSC auth key"})
				return
			}

			repeater.Owner = user
			repeater.OwnerID = user.ID
			err := db.Preload("Owner").Create(&repeater).Error
			if err != nil {
				slog.Error("Error creating repeater", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating repeater"})
				return
			}
			dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
			if ok {
				go dmrHub.ReloadRepeater(context.Background(), repeater.ID)
			}
			c.JSON(http.StatusOK, gin.H{"message": "Repeater created", "password": repeater.Password})
			return
		}

		// MMDVM repeater creation (existing logic)

		// if json.RadioID is a hotspot, then it will be 7 or 9 digits long and be prefixed by the userID
		hotspotRegex := regexp.MustCompile(`^` + fmt.Sprintf("%d", userID) + `([0][1-9]|[1-9][0-9])?$`)

		switch {
		case repeaterIDRegex.MatchString(fmt.Sprintf("%d", json.RadioID)):
			repeater.Hotspot = false
			if !config.DMR.DisableRadioIDValidation {
				if !repeaterdb.IsValidRepeaterID(json.RadioID) {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater ID is not valid"})
					return
				}
				if !repeaterdb.ValidRepeaterCallsign(json.RadioID, user.Callsign) {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Repeater ID does not match assigned callsign"})
					return
				}
			}
			r, ok := repeaterdb.Get(json.RadioID)
			if !ok {
				slog.Error("Error getting repeater from database")
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeater from database"})
				return
			}
			repeater.Callsign = r.Callsign
			if r.ColorCode > 255 {
				slog.Error("Color code out of range for uint8", "colorCode", r.ColorCode)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Color code out of range"})
				return
			}
			repeater.ColorCode = uint8(r.ColorCode)
			// Location is a string with r.City, r.State, and r.Country, set repeater.Location
			repeater.Location = r.City + ", " + r.State + ", " + r.Country
			repeater.Description = r.MapInfo
			// r.Frequency is a string in MHz with a decimal, convert to an int in Hz and set repeater.RXFrequency
			mhZFloat, parseErr := strconv.ParseFloat(r.Frequency, 32)
			if parseErr != nil {
				slog.Error("Error converting frequency to float", "error", parseErr)
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
				slog.Error("Error converting offset to float", "offset", r.Offset, "error", parseErr)
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

		repeater.ID = json.RadioID

		// Generate a random password
		const randLen = 12
		const randNum = 4
		const randSpecial = 0
		repeater.Password, err = utils.RandomPassword(randLen, randNum, randSpecial)
		if err != nil {
			slog.Error("Failed to generate a repeater password", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to generate a repeater password"})
			return
		}

		// Find user by userID
		repeater.Owner = user
		repeater.OwnerID = user.ID
		err := db.Preload("Owner").Create(&repeater).Error
		if err != nil {
			slog.Error("Error creating repeater", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating repeater"})
			return
		}
		dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
		if ok {
			go dmrHub.ReloadRepeater(context.Background(), repeater.ID)
		}
		c.JSON(http.StatusOK, gin.H{"message": "Repeater created", "password": repeater.Password})
	}
}

func POSTRepeaterLink(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	linkType := c.Param("type")
	slot := c.Param("slot")

	// Validate link type and slot
	if linkType != LinkTypeDynamic && linkType != LinkTypeStatic {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link type"})
		return
	}
	if slot != "1" && slot != "2" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid slot"})
		return
	}

	// Validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate talkgroup ID
	talkgroupID, err := validateTalkgroupID(c.Param("target"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate and fetch repeater
	repeater, err := validateAndFetchRepeater(db, repeaterID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			slog.Error("Error validating repeater", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding repeater"})
		}
		return
	}

	// Validate and fetch talkgroup
	talkgroup, err := validateAndFetchTalkgroup(db, talkgroupID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			slog.Error("Error validating talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error finding talkgroup"})
		}
		return
	}

	switch linkType {
	case LinkTypeDynamic:
		switch slot {
		case "1":
			// Set TS1DynamicTalkgroup association on repeater to target
			repeater.TS1DynamicTalkgroup = *talkgroup
			repeater.TS1DynamicTalkgroupID = &talkgroup.ID
		case "2":
			// Set TS2DynamicTalkgroup association on repeater to target
			repeater.TS2DynamicTalkgroup = *talkgroup
			repeater.TS2DynamicTalkgroupID = &talkgroup.ID
		}
	case LinkTypeStatic:
		switch slot {
		case "1":
			// Append TS1StaticTalkgroups association on repeater to target
			err := db.Model(repeater).Association("TS1StaticTalkgroups").Append(talkgroup)
			if err != nil {
				slog.Error("Error appending TS1StaticTalkgroups", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error appending TS1StaticTalkgroups"})
				return
			}
		case "2":
			// Append TS2StaticTalkgroups association on repeater to target
			err := db.Model(repeater).Association("TS2StaticTalkgroups").Append(talkgroup)
			if err != nil {
				slog.Error("Error appending TS2StaticTalkgroups", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error appending TS2StaticTalkgroups"})
				return
			}
		}
	}
	err = db.Save(repeater).Error
	if err != nil {
		slog.Error("Error saving repeater", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
		return
	}
	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if ok {
		go dmrHub.ReloadRepeater(context.Background(), repeater.ID)
	}
}

// validateRepeaterID validates and parses a repeater ID from a parameter
func validateRepeaterID(idParam string) (uint, error) {
	idUint64, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid repeater ID")
	}
	return uint(idUint64), nil
}

// validateTalkgroupID validates and parses a talkgroup ID from a parameter
func validateTalkgroupID(idParam string) (uint, error) {
	idUint64, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid talkgroup ID")
	}
	return uint(idUint64), nil
}

// validateAndFetchRepeater validates that a repeater exists and returns it
func validateAndFetchRepeater(db *gorm.DB, repeaterID uint) (*models.Repeater, error) {
	repeaterExists, err := models.RepeaterIDExists(db, repeaterID)
	if err != nil {
		return nil, fmt.Errorf("error validating repeater: %w", err)
	}
	if !repeaterExists {
		return nil, fmt.Errorf("repeater does not exist")
	}

	repeater, err := models.FindRepeaterByID(db, repeaterID)
	if err != nil {
		return nil, fmt.Errorf("error finding repeater: %w", err)
	}

	return &repeater, nil
}

// validateAndFetchTalkgroup validates that a talkgroup exists and returns it
func validateAndFetchTalkgroup(db *gorm.DB, talkgroupID uint) (*models.Talkgroup, error) {
	talkgroupExists, err := models.TalkgroupIDExists(db, talkgroupID)
	if err != nil {
		return nil, fmt.Errorf("error validating talkgroup: %w", err)
	}
	if !talkgroupExists {
		return nil, fmt.Errorf("talkgroup does not exist")
	}

	talkgroup, err := models.FindTalkgroupByID(db, talkgroupID)
	if err != nil {
		return nil, fmt.Errorf("error finding talkgroup: %w", err)
	}

	return &talkgroup, nil
}

// unlinkParams holds the parameters for unlinking operations
type unlinkParams struct {
	repeaterID  uint
	talkgroupID uint
	linkType    string
	slot        string
}

// validateUnlinkParams validates the input parameters for the unlink operation
func validateUnlinkParams(c *gin.Context) (*unlinkParams, error) {
	params := &unlinkParams{
		linkType: c.Param("type"),
		slot:     c.Param("slot"),
	}

	// Validate link type
	if params.linkType != LinkTypeDynamic && params.linkType != LinkTypeStatic {
		return nil, fmt.Errorf("invalid link type")
	}

	// Validate slot
	if params.slot != "1" && params.slot != "2" {
		return nil, fmt.Errorf("invalid slot")
	}

	// Parse and validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		return nil, err
	}
	params.repeaterID = repeaterID

	// Parse and validate talkgroup ID
	talkgroupID, err := validateTalkgroupID(c.Param("target"))
	if err != nil {
		return nil, err
	}
	params.talkgroupID = talkgroupID

	return params, nil
}

// validateAndFetchEntities validates that the repeater and talkgroup exist and returns them
func validateAndFetchEntities(db *gorm.DB, params *unlinkParams) (*models.Repeater, *models.Talkgroup, error) {
	// Validate and fetch talkgroup
	talkgroup, err := validateAndFetchTalkgroup(db, params.talkgroupID)
	if err != nil {
		return nil, nil, err
	}

	// Validate and fetch repeater
	repeater, err := validateAndFetchRepeater(db, params.repeaterID)
	if err != nil {
		return nil, nil, err
	}

	return repeater, talkgroup, nil
}

// unlinkDynamicTalkgroup handles unlinking dynamic talkgroups
func unlinkDynamicTalkgroup(db *gorm.DB, repeater *models.Repeater, talkgroup *models.Talkgroup, slot string) error {
	switch slot {
	case "1":
		if repeater.TS1DynamicTalkgroupID == nil || *repeater.TS1DynamicTalkgroupID != talkgroup.ID {
			return fmt.Errorf("talkgroup is not linked to repeater")
		}
		repeater.TS1DynamicTalkgroup = models.Talkgroup{}
		repeater.TS1DynamicTalkgroupID = nil
	case "2":
		if repeater.TS2DynamicTalkgroupID == nil || *repeater.TS2DynamicTalkgroupID != talkgroup.ID {
			return fmt.Errorf("talkgroup is not linked to repeater")
		}
		repeater.TS2DynamicTalkgroup = models.Talkgroup{}
		repeater.TS2DynamicTalkgroupID = nil
	}

	return db.Save(repeater).Error
}

// unlinkStaticTalkgroup handles unlinking static talkgroups
func unlinkStaticTalkgroup(db *gorm.DB, repeater *models.Repeater, talkgroup *models.Talkgroup, slot string) error {
	switch slot {
	case "1":
		if !isStaticTalkgroupLinked(repeater.TS1StaticTalkgroups, talkgroup.ID) {
			return fmt.Errorf("talkgroup is not linked to repeater")
		}
		err := db.Model(repeater).Association("TS1StaticTalkgroups").Delete(talkgroup)
		if err != nil {
			return fmt.Errorf("error deleting TS1StaticTalkgroups: %w", err)
		}
	case "2":
		if !isStaticTalkgroupLinked(repeater.TS2StaticTalkgroups, talkgroup.ID) {
			return fmt.Errorf("talkgroup is not linked to repeater")
		}
		err := db.Model(repeater).Association("TS2StaticTalkgroups").Delete(talkgroup)
		if err != nil {
			return fmt.Errorf("error deleting TS2StaticTalkgroups: %w", err)
		}
	}

	return db.Save(repeater).Error
}

// isStaticTalkgroupLinked checks if a talkgroup is linked to static talkgroups
func isStaticTalkgroupLinked(staticTalkgroups []models.Talkgroup, talkgroupID uint) bool {
	for _, tg := range staticTalkgroups {
		if tg.ID == talkgroupID {
			return true
		}
	}
	return false
}

func POSTRepeaterUnlink(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Validate parameters
	params, err := validateUnlinkParams(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate and fetch entities
	repeater, talkgroup, err := validateAndFetchEntities(db, params)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			slog.Error("Error validating entities", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error validating entities"})
		}
		return
	}

	// Perform unlink operation
	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if !ok {
		slog.Error("Hub cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	switch params.linkType {
	case LinkTypeDynamic:
		err = unlinkDynamicTalkgroup(db, repeater, talkgroup, params.slot)
	case LinkTypeStatic:
		err = unlinkStaticTalkgroup(db, repeater, talkgroup, params.slot)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not linked") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			slog.Error("Error unlinking talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
		}
		return
	}

	go dmrHub.ReloadRepeater(context.Background(), repeater.ID)
	c.JSON(http.StatusOK, gin.H{"message": "Timeslot unlinked"})
}

func PATCHRepeater(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var json apimodels.RepeaterPatch
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "PATCHRepeater", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
		return
	}

	repeater, err := validateAndFetchRepeater(db, repeaterID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			slog.Error("Error validating repeater", "function", "PATCHRepeater", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting repeater"})
		}
		return
	}

	if json.SimplexRepeater != nil {
		repeater.SimplexRepeater = *json.SimplexRepeater
	}

	err = db.Save(repeater).Error
	if err != nil {
		slog.Error("Error saving repeater", "function", "PATCHRepeater", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving repeater"})
		return
	}

	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if !ok {
		slog.Error("Hub cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	go dmrHub.ReloadRepeater(context.Background(), repeater.ID)
	c.JSON(http.StatusOK, gin.H{"message": "Repeater updated"})
}
