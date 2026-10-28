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
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	msgErrGettingRules       = "Error getting the rules of the repeater"
	msgErrSavingRules        = "Error saving the rules of the repeater"
	msgBadTalkgroupMode      = "Talkgroup mode must be off, block or allow"
	msgTalkgroupsWithoutMode = "Talkgroups need a talkgroup mode"
	msgNoTalkgroups          = "List at least one talkgroup"
	msgNoSuchUser            = "User does not exist"
	msgNoSuchTalkgroup       = "Talkgroup does not exist"
)

// GETRepeaterRules answers the routing rules of a repeater.
func GETRepeaterRules(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, ok := rulesRepeater(c, db)
	if !ok {
		return
	}
	rules, err := repeaterRules(db, id)
	if err != nil {
		slog.Error("Error getting the rules of a repeater", "repeaterID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRules})
		return
	}
	c.JSON(http.StatusOK, rules)
}

// PATCHRepeaterRules changes the routing rules of a repeater and answers
// them.
func PATCHRepeaterRules(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, ok := rulesRepeater(c, db)
	if !ok {
		return
	}
	var patch apimodels.RepeaterRulesPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	if status, msg := checkRulesPatch(db, &patch); status != http.StatusOK {
		c.JSON(status, gin.H{errorKey: msg})
		return
	}
	if err := saveRulesPatch(db, id, &patch); err != nil {
		slog.Error("Error saving the rules of a repeater", "repeaterID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingRules})
		return
	}
	if dmrHub, ok := c.MustGet("Hub").(*hub.Hub); ok && dmrHub != nil {
		dmrHub.ReloadRepeater(c.Request.Context(), id)
	}
	rules, err := repeaterRules(db, id)
	if err != nil {
		slog.Error("Error getting the rules of a repeater", "repeaterID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRules})
		return
	}
	c.JSON(http.StatusOK, rules)
}

func rulesRepeater(c *gin.Context, db *gorm.DB) (uint, bool) {
	id, err := validateRepeaterID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return 0, false
	}
	exists, err := models.RepeaterIDExists(db, id)
	if err != nil {
		slog.Error("Error finding a repeater", "repeaterID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeater})
		return 0, false
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoRepeater})
		return 0, false
	}
	return id, true
}

func checkRulesPatch(db *gorm.DB, patch *apimodels.RepeaterRulesPatch) (int, string) {
	for _, users := range []*[]uint{patch.TS1Users, patch.TS2Users} {
		if users == nil {
			continue
		}
		ok, err := allExist(db, &models.User{}, *users)
		if err != nil {
			slog.Error("Error finding users", "error", err)
			return http.StatusInternalServerError, msgErrSavingRules
		}
		if !ok {
			return http.StatusBadRequest, msgNoSuchUser
		}
	}
	if patch.TalkgroupMode == nil {
		if patch.Talkgroups != nil {
			return http.StatusBadRequest, msgTalkgroupsWithoutMode
		}
		return http.StatusOK, ""
	}
	mode := *patch.TalkgroupMode
	switch mode {
	case apimodels.TalkgroupModeOff:
		return http.StatusOK, ""
	case apimodels.TalkgroupModeBlock, apimodels.TalkgroupModeAllow:
	default:
		return http.StatusBadRequest, msgBadTalkgroupMode
	}
	if patch.Talkgroups == nil || len(*patch.Talkgroups) == 0 {
		return http.StatusBadRequest, msgNoTalkgroups
	}
	ok, err := allExist(db, &models.Talkgroup{}, *patch.Talkgroups)
	if err != nil {
		slog.Error("Error finding talkgroups", "error", err)
		return http.StatusInternalServerError, msgErrSavingRules
	}
	if !ok {
		return http.StatusBadRequest, msgNoSuchTalkgroup
	}
	return http.StatusOK, ""
}

func allExist(db *gorm.DB, model any, ids []uint) (bool, error) {
	if len(ids) == 0 {
		return true, nil
	}
	unique := slices.Compact(slices.Sorted(slices.Values(ids)))
	var n int64
	if err := db.Model(model).Where("id IN ?", unique).Count(&n).Error; err != nil {
		return false, err //nolint:wrapcheck // the caller logs it
	}
	return n == int64(len(unique)), nil
}

func saveRulesPatch(db *gorm.DB, id uint, patch *apimodels.RepeaterRulesPatch) error {
	return db.Transaction(func(tx *gorm.DB) error { //nolint:wrapcheck // the caller logs it
		if patch.TS1Users != nil || patch.TS2Users != nil {
			current, err := models.ListRoutingRules(tx, id)
			if err != nil {
				return err //nolint:wrapcheck // ListRoutingRules wraps its errors
			}
			var rules []models.RoutingRule
			for slot, users := range map[uint8]*[]uint{1: patch.TS1Users, 2: patch.TS2Users} {
				if users != nil {
					for _, u := range *users {
						rules = append(rules, models.RoutingRule{Kind: models.RoutingRuleSlotUser, Slot: slot, SubjectID: u})
					}
					continue
				}
				for _, r := range current {
					if r.Kind == models.RoutingRuleSlotUser && r.Slot == slot {
						rules = append(rules, r)
					}
				}
			}
			if err := models.ReplaceRoutingRules(tx, id, models.SlotRuleKinds(), rules); err != nil {
				return err //nolint:wrapcheck // ReplaceRoutingRules wraps its errors
			}
		}
		if patch.TalkgroupMode == nil {
			return nil
		}
		kind := models.RoutingRuleBlockTalkgroup
		if *patch.TalkgroupMode == apimodels.TalkgroupModeAllow {
			kind = models.RoutingRuleAllowTalkgroup
		}
		var rules []models.RoutingRule
		if *patch.TalkgroupMode != apimodels.TalkgroupModeOff && patch.Talkgroups != nil {
			for _, tg := range *patch.Talkgroups {
				rules = append(rules, models.RoutingRule{Kind: kind, SubjectID: tg})
			}
		}
		return models.ReplaceRoutingRules(tx, id, models.TalkgroupRuleKinds(), rules) //nolint:wrapcheck // ReplaceRoutingRules wraps its errors
	})
}

func repeaterRules(db *gorm.DB, id uint) (apimodels.RepeaterRules, error) {
	out := apimodels.RepeaterRules{
		TS1Users: []models.UserRef{}, TS2Users: []models.UserRef{},
		TalkgroupMode: apimodels.TalkgroupModeOff, Talkgroups: []apimodels.TalkgroupRef{},
	}
	rules, err := models.ListRoutingRules(db, id)
	if err != nil {
		return out, err //nolint:wrapcheck // ListRoutingRules wraps its errors
	}
	var userIDs, tgIDs []uint
	for _, r := range rules {
		switch r.Kind {
		case models.RoutingRuleSlotUser:
			userIDs = append(userIDs, r.SubjectID)
		case models.RoutingRuleBlockTalkgroup, models.RoutingRuleAllowTalkgroup:
			tgIDs = append(tgIDs, r.SubjectID)
		}
	}
	callsigns := map[uint]string{}
	if len(userIDs) > 0 {
		var users []models.User
		if err := db.Select("id", "callsign").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return out, err //nolint:wrapcheck // the caller logs it
		}
		for _, u := range users {
			callsigns[u.ID] = u.Callsign
		}
	}
	names := map[uint]string{}
	if len(tgIDs) > 0 {
		var tgs []models.Talkgroup
		if err := db.Select("id", "name").Where("id IN ?", tgIDs).Find(&tgs).Error; err != nil {
			return out, err //nolint:wrapcheck // the caller logs it
		}
		for _, tg := range tgs {
			names[tg.ID] = tg.Name
		}
	}
	for _, r := range rules {
		switch r.Kind {
		case models.RoutingRuleSlotUser:
			ref := models.UserRef{ID: r.SubjectID, Callsign: callsigns[r.SubjectID]}
			switch r.Slot {
			case 1:
				out.TS1Users = append(out.TS1Users, ref)
			case 2:
				out.TS2Users = append(out.TS2Users, ref)
			}
		case models.RoutingRuleBlockTalkgroup, models.RoutingRuleAllowTalkgroup:
			out.TalkgroupMode = apimodels.TalkgroupModeBlock
			if r.Kind == models.RoutingRuleAllowTalkgroup {
				out.TalkgroupMode = apimodels.TalkgroupModeAllow
			}
			out.Talkgroups = append(out.Talkgroups, apimodels.TalkgroupRef{ID: r.SubjectID, Name: names[r.SubjectID]})
		}
	}
	return out, nil
}

// GETRepeaterRuleUser answers the user whose DMR ID, added radio ID or
// callsign is the query q, for a slot lock of the repeater.
func GETRepeaterRuleUser(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	if _, ok := rulesRepeater(c, db); !ok {
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	var users []models.User
	if id, err := strconv.ParseUint(q, 10, 32); err == nil {
		owner, found, err := models.RadioIDOwner(db, uint(id))
		if err != nil {
			slog.Error("Error finding a user", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
			return
		}
		if found {
			if err := db.Select("id", "callsign").Where("id = ?", owner).Limit(1).Find(&users).Error; err != nil {
				slog.Error("Error finding a user", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
				return
			}
		}
	} else if q != "" {
		if err := db.Select("id", "callsign").Where("UPPER(callsign) = ?", strings.ToUpper(q)).Order("id").Limit(1).Find(&users).Error; err != nil {
			slog.Error("Error finding a user", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
			return
		}
	}
	if len(users) == 0 {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoSuchUser})
		return
	}
	c.JSON(http.StatusOK, models.NewUserRef(&users[0]))
}
