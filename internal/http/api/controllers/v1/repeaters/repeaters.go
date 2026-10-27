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
	encjson "encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	errorKey               = "error"
	msgAuthFailed          = "Authentication failed"
	msgTryAgainLater       = "Try again later"
	msgErrGettingRepeater  = "Error getting repeater"
	messageKey             = "message"
	msgInvalidJSON         = "JSON data is invalid"
	msgErrSavingRepeater   = "Error saving repeater"
	msgErrGettingRepeaters = "Error getting repeaters"
	keyRepeaters           = "repeaters"
	keyTotal               = "total"
)

const (
	LinkTypeDynamic = "dynamic"
	LinkTypeStatic  = "static"
)

// repeaterIDRegex validates that a radio ID is a 6-digit repeater ID.
// Compiled once at package level to avoid re-compilation on every request.
var repeaterIDRegex = regexp.MustCompile(`^[0-9]{6}$`)

// hotspotRegex validates that a radio ID is a valid hotspot ID for the user. It must be 7 or 9 digits long and start with the userID. Compiled once at package level to avoid re-compilation on every request.
var hotspotRegex = regexp.MustCompile(`^([0-9]{7}|[0-9]{9})$`)

func GETRepeaters(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}
	repeaters, err := models.ListRepeaters(db)
	if err != nil {
		slog.Error(msgErrGettingRepeaters, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeaters})
		return
	}

	count, err := models.CountRepeaters(cDb)
	if err != nil {
		slog.Error(msgErrGettingRepeaters, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeaters})
		return
	}

	c.JSON(http.StatusOK, gin.H{keyTotal: count, keyRepeaters: repeaters})
}

func GETMyRepeaters(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}
	session := sessions.Default(c)

	userID := session.Get("user_id")
	if userID == nil {
		slog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		slog.Error("Unable to convert userID to uint", "function", "GETMyRepeaters", "userID", userID)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}

	// Get all repeaters owned by user
	repeaters, err := models.GetUserRepeaters(db, uid)
	if err != nil {
		slog.Error("Error getting repeaters owned by user", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting repeaters owned by user"})
		return
	}

	count, err := models.CountUserRepeaters(cDb, uid)
	if err != nil {
		slog.Error("Error getting repeaters owned by user", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting repeaters owned by user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{keyTotal: count, keyRepeaters: repeaters})
}

func GETRepeater(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	// Validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}

	// Validate and fetch repeater
	repeater, err := validateAndFetchRepeater(db, repeaterID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		} else {
			slog.Error("Error validating repeater", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeater})
		}
		return
	}
	if grant, viaToken := utils.Grant(c); viaToken && repeater.OwnerID != grant.UserID {
		c.JSON(http.StatusForbidden, gin.H{errorKey: middleware.MsgNotRepeaterOwner})
		return
	}

	c.JSON(http.StatusOK, *repeater)
}

func DELETERepeater(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	// Validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}

	var owners []uint
	if err := db.Model(&models.Repeater{}).Where("id = ? AND owner_id IS NOT NULL", repeaterID).Pluck("owner_id", &owners).Error; err != nil {
		slog.Error("Error finding the owner of a repeater", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error deleting repeater"})
		return
	}

	err = models.DeleteRepeater(db, repeaterID)
	if err != nil {
		slog.Error("Error deleting repeater", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error deleting repeater"})
		return
	}
	if dmrHub, ok := c.MustGet("Hub").(*hub.Hub); ok && dmrHub != nil {
		dmrHub.ReloadRepeater(c.Request.Context(), repeaterID)
	}
	for _, owner := range owners {
		publishOwnerChanged(c, owner)
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "Repeater deleted"})
}

func POSTRepeaterTalkgroups(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	// Validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}

	var json apimodels.RepeaterTalkgroupsPost
	var fields map[string]encjson.RawMessage
	err = c.ShouldBindBodyWithJSON(&json)
	if err == nil {
		err = c.ShouldBindBodyWithJSON(&fields)
	}
	if err != nil {
		slog.Error(msgInvalidJSON, "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	_, setTS1Dynamic := fields["ts1_dynamic_talkgroup"]
	_, setTS2Dynamic := fields["ts2_dynamic_talkgroup"]

	// Validate and fetch repeater
	repeater, err := validateAndFetchRepeater(db, repeaterID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		} else {
			slog.Error("Error validating repeater", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeater})
		}
		return
	}

	if missing, err := missingTalkgroup(db, &json); err != nil {
		slog.Error("Error validating talkgroups", "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	} else if missing != 0 {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: fmt.Sprintf("talkgroup %d does not exist", missing)})
		return
	}

	err = db.Model(repeater).Association("TS1StaticTalkgroups").Replace(json.TS1StaticTalkgroups)
	if err != nil {
		slog.Error("Error updating TS1StaticTalkgroups", "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error updating TS1StaticTalkgroups"})
		return
	}
	repeater.TS1StaticTalkgroups = json.TS1StaticTalkgroups
	err = db.Model(repeater).Association("TS2StaticTalkgroups").Replace(json.TS2StaticTalkgroups)
	if err != nil {
		slog.Error("Error updating TS2StaticTalkgroups", "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error updating TS2StaticTalkgroups"})
		return
	}
	repeater.TS2StaticTalkgroups = json.TS2StaticTalkgroups

	switch {
	case !setTS1Dynamic:
	case json.TS1DynamicTalkgroup.ID == 0:
		repeater.TS1DynamicTalkgroupID = nil
		err = db.Model(repeater).Association("TS1DynamicTalkgroup").Delete(&repeater.TS1DynamicTalkgroup)
		if err != nil {
			slog.Error("Error deleting TS1DynamicTalkgroup", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error deleting TS1DynamicTalkgroup"})
			return
		}
	default:
		repeater.TS1DynamicTalkgroupID = &json.TS1DynamicTalkgroup.ID
		repeater.TS1DynamicTalkgroup = json.TS1DynamicTalkgroup
		err = db.Model(repeater).Association("TS1DynamicTalkgroup").Replace(&json.TS1DynamicTalkgroup)
		if err != nil {
			slog.Error("Error updating TS1DynamicTalkgroup", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error updating TS1DynamicTalkgroup"})
			return
		}
	}

	switch {
	case !setTS2Dynamic:
	case json.TS2DynamicTalkgroup.ID == 0:
		repeater.TS2DynamicTalkgroupID = nil
		err = db.Model(repeater).Association("TS2DynamicTalkgroup").Delete(&repeater.TS2DynamicTalkgroup)
		if err != nil {
			slog.Error("Error deleting TS2DynamicTalkgroup", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error deleting TS2DynamicTalkgroup"})
			return
		}
	default:
		repeater.TS2DynamicTalkgroupID = &json.TS2DynamicTalkgroup.ID
		repeater.TS2DynamicTalkgroup = json.TS2DynamicTalkgroup
		err = db.Model(repeater).Association("TS2DynamicTalkgroup").Replace(&json.TS2DynamicTalkgroup)
		if err != nil {
			slog.Error("Error updating TS2DynamicTalkgroup", "function", "POSTRepeaterTalkgroups", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error updating TS2DynamicTalkgroup"})
			return
		}
	}

	err = db.Save(repeater).Error
	if err != nil {
		slog.Error(msgErrSavingRepeater, "function", "POSTRepeaterTalkgroups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingRepeater})
		return
	}
	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if !ok {
		slog.Error("Hub cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	dmrHub.ReloadRepeater(c.Request.Context(), repeater.ID)
	c.JSON(http.StatusOK, gin.H{messageKey: "Repeater talkgroups updated"})
}

// missingTalkgroup returns the first talkgroup of post that does not
// exist, or 0 when they all do. Without it, saving the associations would
// create a blank talkgroup for each ID that does not exist.
func missingTalkgroup(db *gorm.DB, post *apimodels.RepeaterTalkgroupsPost) (uint, error) {
	var ids []uint
	for _, tg := range post.TS1StaticTalkgroups {
		ids = append(ids, tg.ID)
	}
	for _, tg := range post.TS2StaticTalkgroups {
		ids = append(ids, tg.ID)
	}
	for _, tg := range []models.Talkgroup{post.TS1DynamicTalkgroup, post.TS2DynamicTalkgroup} {
		if tg.ID != 0 {
			ids = append(ids, tg.ID)
		}
	}
	found, err := models.FindTalkgroupsByIDs(db, ids)
	if err != nil {
		return 0, fmt.Errorf("error finding talkgroups: %w", err)
	}
	for _, id := range ids {
		if !slices.ContainsFunc(found, func(tg models.Talkgroup) bool { return tg.ID == id }) {
			return id, nil
		}
	}
	return 0, nil
}

//nolint:gocyclo
func POSTRepeater(c *gin.Context) {
	session := sessions.Default(c)
	usID := session.Get("user_id")
	if usID == nil {
		slog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
		return
	}
	userID, ok := usID.(uint)
	if !ok {
		slog.Error("userID cast failed")
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgAuthFailed})
		return
	}
	db, ok := utils.GetDB(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	config, ok := utils.GetConfig(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}

	user, err := models.FindUserByID(db, userID)
	if err != nil {
		slog.Error("Error getting user", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting user"})
		return
	}

	var json apimodels.RepeaterPost
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error(msgInvalidJSON, "function", "POSTRepeater", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	} else {
		// Default type to MMDVM if not specified
		if json.Type == "" {
			json.Type = models.RepeaterTypeMMDVM
		}
		if json.Type != models.RepeaterTypeMMDVM && json.Type != models.RepeaterTypeIPSC {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid repeater type"})
			return
		}
		if json.Type == models.RepeaterTypeIPSC && !config.DMR.IPSC.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "IPSC is not enabled on this server"})
			return
		}

		var repeater models.Repeater
		repeater.Type = json.Type
		repeater.SimplexRepeater = json.SimplexRepeater
		repeater.ID = json.RadioID
		repeater.Owner = user
		repeater.OwnerID = user.ID

		switch json.Type {
		case models.RepeaterTypeIPSC:
			// Generate a random hex auth key (20 bytes = 40 hex chars) for IPSC HMAC-SHA1
			const ipscKeyLen = 40
			repeater.Password, err = utils.RandomHexString(ipscKeyLen)
			if err != nil {
				slog.Error("Failed to generate an IPSC auth key", "error", err)
				c.JSON(http.StatusBadRequest, gin.H{errorKey: "Failed to generate an IPSC auth key"})
				return
			}

		case models.RepeaterTypeMMDVM:
			// Generate a random password
			const randLen = 12
			const randNum = 4
			const randSpecial = 0
			repeater.Password, err = utils.RandomPassword(randLen, randNum, randSpecial)
			if err != nil {
				slog.Error("Failed to generate a repeater password", "error", err)
				c.JSON(http.StatusBadRequest, gin.H{errorKey: "Failed to generate a repeater password"})
				return
			}
		}

		switch {
		case repeaterIDRegex.MatchString(fmt.Sprintf("%d", json.RadioID)):
			if !config.DMR.DisableRadioIDValidation {
				if !repeaterdb.IsValidRepeaterID(json.RadioID) {
					c.JSON(http.StatusBadRequest, gin.H{errorKey: "Repeater ID is not valid"})
					return
				}
				if !repeaterdb.ValidRepeaterCallsign(json.RadioID, user.Callsign) {
					c.JSON(http.StatusBadRequest, gin.H{errorKey: "Repeater ID does not match assigned callsign"})
					return
				}

				r, ok := repeaterdb.Get(json.RadioID)
				if !ok {
					slog.Error("Error getting repeater from database")
					c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting repeater from database"})
					return
				}
				repeater.Callsign = r.Callsign
				if r.ColorCode > 255 {
					slog.Error("Color code out of range for uint8", "colorCode", r.ColorCode)
					c.JSON(http.StatusBadRequest, gin.H{errorKey: "Color code out of range"})
					return
				}
				repeater.ColorCode = uint8(r.ColorCode)
				repeater.City = r.City
				repeater.State = r.State
				repeater.Country = r.Country
				repeater.Description = r.MapInfo
				// r.Frequency is a string in MHz with a decimal, convert to an int in Hz and set repeater.RXFrequency
				mhZFloat, parseErr := strconv.ParseFloat(r.Frequency, 32)
				if parseErr != nil {
					slog.Error("Error converting frequency to float", "error", parseErr)
					c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error converting frequency to float"})
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
					c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error converting offset to float"})
					return
				}
				// convert the offset to an int in Hz
				offsetInt := uint(offsetFloat * mHzToHz)
				if positiveOffset {
					repeater.RXFrequency = repeater.TXFrequency + offsetInt
				} else {
					repeater.RXFrequency = repeater.TXFrequency - offsetInt
				}
			}
		case hotspotRegex.MatchString(fmt.Sprintf("%d", json.RadioID)):
			if !config.DMR.DisableRadioIDValidation {
				// check that first 7 digits of hotspot ID match userID
				userIDStr := fmt.Sprintf("%d", userID)
				if !strings.HasPrefix(fmt.Sprintf("%d", json.RadioID), userIDStr) {
					c.JSON(http.StatusBadRequest, gin.H{errorKey: "Hotspot ID must start with user ID"})
					return
				}
			}
			repeater.Callsign = user.Callsign
		default:
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "RadioID is invalid"})
			return
		}

		err := db.Preload("Owner").Create(&repeater).Error
		if err != nil {
			slog.Error("Error creating repeater", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error creating repeater"})
			return
		}
		dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
		if ok {
			dmrHub.ReloadRepeater(c.Request.Context(), repeater.ID)
		}
		publishOwnerChanged(c, repeater.OwnerID)
		c.JSON(http.StatusOK, gin.H{messageKey: "Repeater created", "password": repeater.Password})
	}
}

func POSTRepeaterLink(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	linkType := c.Param("type")
	slot := c.Param("slot")

	// Validate link type and slot
	if linkType != LinkTypeDynamic && linkType != LinkTypeStatic {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid link type"})
		return
	}
	if slot != "1" && slot != "2" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid slot"})
		return
	}

	// Validate repeater ID
	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}

	// Validate talkgroup ID
	talkgroupID, err := validateTalkgroupID(c.Param("target"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}

	// Validate and fetch repeater
	repeater, err := validateAndFetchRepeater(db, repeaterID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		} else {
			slog.Error("Error validating repeater", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error finding repeater"})
		}
		return
	}

	// Validate and fetch talkgroup
	talkgroup, err := validateAndFetchTalkgroup(db, talkgroupID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		} else {
			slog.Error("Error validating talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error finding talkgroup"})
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
				c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error appending TS1StaticTalkgroups"})
				return
			}
		case "2":
			// Append TS2StaticTalkgroups association on repeater to target
			err := db.Model(repeater).Association("TS2StaticTalkgroups").Append(talkgroup)
			if err != nil {
				slog.Error("Error appending TS2StaticTalkgroups", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error appending TS2StaticTalkgroups"})
				return
			}
		}
	}
	err = db.Save(repeater).Error
	if err != nil {
		slog.Error(msgErrSavingRepeater, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingRepeater})
		return
	}
	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if ok {
		dmrHub.ReloadRepeater(c.Request.Context(), repeater.ID)
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
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	// Validate parameters
	params, err := validateUnlinkParams(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}

	// Validate and fetch entities
	repeater, talkgroup, err := validateAndFetchEntities(db, params)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		} else {
			slog.Error("Error validating entities", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error validating entities"})
		}
		return
	}

	// Perform unlink operation
	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if !ok {
		slog.Error("Hub cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
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
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		} else {
			slog.Error("Error unlinking talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingRepeater})
		}
		return
	}

	dmrHub.ReloadRepeater(c.Request.Context(), repeater.ID)
	c.JSON(http.StatusOK, gin.H{messageKey: "Timeslot unlinked"})
}

func PATCHRepeater(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	repeaterID, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}

	var json apimodels.RepeaterPatch
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error(msgInvalidJSON, "function", "PATCHRepeater", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}

	repeater, err := validateAndFetchRepeater(db, repeaterID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		} else {
			slog.Error("Error validating repeater", "function", "PATCHRepeater", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeater})
		}
		return
	}

	if json.SimplexRepeater != nil {
		repeater.SimplexRepeater = *json.SimplexRepeater
	}
	if json.Trusted != nil && !requesterIsAdmin(c, db) {
		c.JSON(http.StatusForbidden, gin.H{errorKey: "Only an admin can change whether a repeater is trusted"})
		return
	}
	codecChanged := false
	for _, set := range []struct {
		value *string
		field *string
	}{{json.TS1Codec, &repeater.TS1Codec}, {json.TS2Codec, &repeater.TS2Codec}} {
		if set.value == nil {
			continue
		}
		if !models.ValidCodecSetting(*set.value) {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "The codec of a timeslot must be auto, codec2 or ambe"})
			return
		}
		if repeater.Type == models.RepeaterTypeIPSC && *set.value == models.CodecCodec2 {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "IPSC repeaters carry only AMBE"})
			return
		}
		codecChanged = codecChanged || *set.field != *set.value
		*set.field = *set.value
	}

	err = db.Save(repeater).Error
	if err == nil && json.Trusted != nil {
		err = db.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", *json.Trusted, repeater.ID).Error
	}
	if err == nil && (json.TS1Codec != nil || json.TS2Codec != nil) {
		err = models.SetRepeaterCodec(db, repeater.ID, repeater.TS1Codec, repeater.TS2Codec)
	}
	if err != nil {
		slog.Error(msgErrSavingRepeater, "function", "PATCHRepeater", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingRepeater})
		return
	}

	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if !ok {
		slog.Error("Hub cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	dmrHub.ReloadRepeater(c.Request.Context(), repeater.ID)
	if codecChanged {
		dmrHub.CodecChanged(repeater.ID)
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "Repeater updated"})
}

func publishOwnerChanged(c *gin.Context, ownerID uint) {
	ps, ok := c.Value("PubSub").(pubsub.PubSub)
	if !ok {
		slog.Error("PubSub not found in context, the websockets of a repeater owner stay open", "user", ownerID)
		return
	}
	if err := websocket.PublishUserChanged(ps, ownerID); err != nil {
		slog.Error("Failed to close the websockets of a repeater owner", "user", ownerID, "error", err)
	}
}

// GETUnassignedRepeaters lists the repeaters with no owner, such as the
// one that DMRHub makes for a modem whose repeater did not exist.
func GETUnassignedRepeaters(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}
	repeaters, err := models.ListUnassignedRepeaters(db)
	if err != nil {
		slog.Error("Error getting the unassigned repeaters", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeaters})
		return
	}
	count, err := models.CountUnassignedRepeaters(cDb)
	if err != nil {
		slog.Error("Error counting the unassigned repeaters", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeaters})
		return
	}
	c.JSON(http.StatusOK, gin.H{keyTotal: count, keyRepeaters: repeaters})
}

// POSTRepeaterOwner assigns the owner of a repeater with no owner.
func POSTRepeaterOwner(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid repeater ID"})
		return
	}
	var json apimodels.RepeaterOwnerPost
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	var user models.User
	if err := db.Select("id").First(&user, json.Owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Owner does not exist"})
			return
		}
		slog.Error("Error finding the owner of a repeater", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	switch err := models.AssignRepeaterOwner(db, uint(id), user.ID); {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: "Repeater does not exist"})
		return
	case errors.Is(err, models.ErrRepeaterHasOwner):
		c.JSON(http.StatusConflict, gin.H{errorKey: "Repeater already has an owner"})
		return
	case err != nil:
		slog.Error("Error assigning the owner of a repeater", "repeaterID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingRepeater})
		return
	}
	if dmrHub, ok := c.MustGet("Hub").(*hub.Hub); ok && dmrHub != nil {
		dmrHub.ReloadRepeater(c.Request.Context(), uint(id))
	}
	publishOwnerChanged(c, user.ID)
	c.JSON(http.StatusOK, gin.H{messageKey: "Owner assigned"})
}

func requesterIsAdmin(c *gin.Context, db *gorm.DB) bool {
	if utils.ViaToken(c) {
		return false
	}
	userID, ok := sessions.Default(c).Get("user_id").(uint)
	if !ok {
		return false
	}
	user, err := models.FindUserByID(db, userID)
	if err != nil {
		return false
	}
	return user.Admin && user.Approved && !user.Suspended
}
