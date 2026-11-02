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

// Package localgroups serves the local groups: groups of repeaters that
// share a local talkgroup.
package localgroups

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	errorKey   = "error"
	messageKey = "message"

	msgTryLater        = "Try again later"
	msgInvalidJSON     = "Invalid JSON"
	msgNoGroup         = "Local group does not exist"
	msgNoRepeater      = "Repeater does not exist"
	msgNotYours        = "You cannot change this local group"
	msgNotRepeaterMine = "You cannot change this repeater"
	msgBadName         = "Give the group a name of 1 to 64 characters"
	msgBadTalkgroup    = "Pick one of the local talkgroups"
	msgBadSlot         = "The timeslot must be 1 or 2"
	msgNumberTaken     = "This repeater is already in a local group on that talkgroup"
	msgAlreadyIn       = "This repeater is already in the group"
	msgNotAsked        = "Nobody asked to add this repeater to the group"
	maxNameLength      = 64
)

type actor struct {
	user  models.User
	admin bool
}

func session(c *gin.Context) (*gorm.DB, actor, bool) {
	db, ok := utils.GetDB(c)
	if !ok {
		return nil, actor{}, false
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return nil, actor{}, false
	}
	return db, actor{user: user, admin: user.Admin || user.SuperAdmin}, true
}

func fail(c *gin.Context, err error, what string) {
	slog.Error(what, "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
}

func localNumbers(c *gin.Context) ([]uint, bool) {
	cfg, ok := utils.GetConfig(c)
	if !ok {
		return nil, false
	}
	return cfg.DMR.Routing.LocalTalkgroups, true
}

func reload(c *gin.Context, ids ...uint) {
	if dmrHub, ok := c.MustGet("Hub").(*hub.Hub); ok && dmrHub != nil {
		for _, id := range ids {
			dmrHub.ReloadRepeater(c.Request.Context(), id)
		}
	}
}

func idParam(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid " + name})
		return 0, false
	}
	return uint(id), true
}

// group loads the group of the id parameter.
func group(c *gin.Context, db *gorm.DB) (models.LocalGroup, bool) {
	id, ok := idParam(c, "id")
	if !ok {
		return models.LocalGroup{}, false
	}
	g, err := models.FindLocalGroup(db, id)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoGroup})
		return g, false
	case err != nil:
		fail(c, err, "Error finding a local group")
		return g, false
	}
	return g, true
}

func (a actor) manages(g models.LocalGroup) bool {
	return a.admin || g.OwnerID == a.user.ID
}

// runs reports whether a runs the repeater whose owner is owner: as its
// owner or as a co-sysop.
func (a actor) runs(db *gorm.DB, repeaterID, owner uint) (bool, error) {
	if owner == a.user.ID {
		return true, nil
	}
	co, err := models.IsRepeaterCosysop(db, repeaterID, a.user.ID)
	if err != nil {
		return false, err //nolint:wrapcheck // IsRepeaterCosysop wraps its errors
	}
	return co, nil
}

// ownerOf returns the owner of the repeater, or false when it does not
// exist.
func ownerOf(db *gorm.DB, repeaterID uint) (uint, bool, error) {
	var owners []uint
	if err := db.Model(&models.Repeater{}).Where("id = ?", repeaterID).Pluck("owner_id", &owners).Error; err != nil {
		return 0, false, err //nolint:wrapcheck // the caller logs it
	}
	if len(owners) == 0 {
		return 0, false, nil
	}
	return owners[0], true, nil
}

func checkGroupPost(c *gin.Context, body apimodels.LocalGroupPost) (string, uint, bool) {
	name := strings.TrimSpace(body.Name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadName})
		return "", 0, false
	}
	numbers, ok := localNumbers(c)
	if !ok {
		return "", 0, false
	}
	if !slices.Contains(numbers, body.Talkgroup) {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadTalkgroup})
		return "", 0, false
	}
	return name, body.Talkgroup, true
}

// GETLocalGroups answers the local groups that the user owns or that a
// repeater of the user is in or asked to join; all of them for an admin.
func GETLocalGroups(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	q := db.Model(&models.LocalGroup{}).Order("talkgroup, name, id")
	if !a.admin {
		mine, err := models.LocalGroupsWithRepeatersRunBy(db, a.user.ID)
		if err != nil {
			fail(c, err, "Error listing local groups")
			return
		}
		q = q.Where("owner_id = ? OR id IN ?", a.user.ID, append(mine, 0))
	}
	var groups []models.LocalGroup
	if err := q.Find(&groups).Error; err != nil {
		fail(c, err, "Error listing local groups")
		return
	}
	out := make([]apimodels.LocalGroup, 0, len(groups))
	for _, g := range groups {
		v, err := view(db, a, g, false)
		if err != nil {
			fail(c, err, "Error showing a local group")
			return
		}
		out = append(out, v)
	}
	numbers, ok := localNumbers(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, apimodels.LocalGroupList{Groups: out, Talkgroups: append([]uint{}, numbers...)})
}

// POSTLocalGroup makes a local group that the user owns.
func POSTLocalGroup(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	var body apimodels.LocalGroupPost
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	name, tg, ok := checkGroupPost(c, body)
	if !ok {
		return
	}
	g := models.LocalGroup{Name: name, Talkgroup: tg, OwnerID: a.user.ID}
	if err := db.Create(&g).Error; err != nil {
		fail(c, err, "Error making a local group")
		return
	}
	answer(c, db, a, g)
}

// GETLocalGroup answers a local group to its owner, an admin, and the
// owners of its repeaters.
func GETLocalGroup(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	g, ok := group(c, db)
	if !ok {
		return
	}
	v, err := view(db, a, g, true)
	if err != nil {
		fail(c, err, "Error showing a local group")
		return
	}
	if !a.manages(g) && !v.Involved {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoGroup})
		return
	}
	c.JSON(http.StatusOK, v)
}

// PATCHLocalGroup renames a local group or moves it to another local
// talkgroup.
func PATCHLocalGroup(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	g, ok := managed(c, db, a)
	if !ok {
		return
	}
	var body apimodels.LocalGroupPost
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	name, tg, ok := checkGroupPost(c, body)
	if !ok {
		return
	}
	err := models.UpdateLocalGroup(db, g.ID, name, tg)
	switch {
	case errors.Is(err, models.ErrLocalNumberTaken):
		c.JSON(http.StatusConflict, gin.H{errorKey: msgNumberTaken})
		return
	case err != nil:
		fail(c, err, "Error saving a local group")
		return
	}
	members, err := models.LocalGroupMembers(db, g.ID)
	if err != nil {
		fail(c, err, "Error listing the repeaters of a local group")
		return
	}
	for _, m := range members {
		reload(c, m.RepeaterID)
	}
	g.Name, g.Talkgroup = name, tg
	answer(c, db, a, g)
}

// DELETELocalGroup deletes a local group.
func DELETELocalGroup(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	g, ok := managed(c, db, a)
	if !ok {
		return
	}
	members, err := models.DeleteLocalGroup(db, g.ID)
	if err != nil {
		fail(c, err, "Error deleting a local group")
		return
	}
	reload(c, members...)
	c.JSON(http.StatusOK, gin.H{messageKey: "Local group deleted"})
}

// POSTLocalGroupRepeater adds a repeater to a local group. A repeater of
// the group owner, or of the user who adds it, joins at once; that of
// another owner joins once its owner says yes.
func POSTLocalGroupRepeater(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	g, ok := managed(c, db, a)
	if !ok {
		return
	}
	var body apimodels.LocalGroupRepeaterPost
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	slot := body.Slot
	if slot == 0 {
		slot = 2
	}
	if slot != 1 && slot != 2 {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadSlot})
		return
	}
	owner, exists, err := ownerOf(db, body.RepeaterID)
	if err != nil {
		fail(c, err, "Error finding a repeater")
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoRepeater})
		return
	}
	runs, err := a.runs(db, body.RepeaterID, owner)
	if err != nil {
		fail(c, err, "Error finding the co-sysops of a repeater")
		return
	}
	state := models.LocalMemberAsked
	if owner == g.OwnerID || runs {
		state = models.LocalMemberIn
	}
	err = models.AddLocalGroupMember(db, models.LocalGroupMember{LocalGroupID: g.ID, RepeaterID: body.RepeaterID, Slot: slot, State: state, AskedByID: a.user.ID})
	switch {
	case errors.Is(err, models.ErrLocalNumberTaken):
		c.JSON(http.StatusConflict, gin.H{errorKey: msgNumberTaken})
		return
	case errors.Is(err, gorm.ErrDuplicatedKey):
		c.JSON(http.StatusConflict, gin.H{errorKey: msgAlreadyIn})
		return
	case err != nil:
		fail(c, err, "Error adding a repeater to a local group")
		return
	}
	reload(c, body.RepeaterID)
	answer(c, db, a, g)
}

// memberRequest loads the group and the repeater of the parameters, and
// the owner of the repeater.
func memberRequest(c *gin.Context, db *gorm.DB) (models.LocalGroup, uint, uint, bool) {
	g, ok := group(c, db)
	if !ok {
		return g, 0, 0, false
	}
	rid, ok := idParam(c, "repeater")
	if !ok {
		return g, 0, 0, false
	}
	owner, exists, err := ownerOf(db, rid)
	if err != nil {
		fail(c, err, "Error finding a repeater")
		return g, 0, 0, false
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoRepeater})
		return g, 0, 0, false
	}
	return g, rid, owner, true
}

func memberError(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotAsked})
		return true
	case err != nil:
		fail(c, err, "Error changing a repeater in a local group")
		return true
	}
	return false
}

// PATCHLocalGroupRepeater sets the timeslot that a repeater takes the
// calls of the group on. The group owner and the repeater owner may.
func PATCHLocalGroupRepeater(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	g, rid, owner, ok := memberRequest(c, db)
	if !ok {
		return
	}
	runs, err := a.runs(db, rid, owner)
	if err != nil {
		fail(c, err, "Error finding the co-sysops of a repeater")
		return
	}
	if !a.manages(g) && !runs {
		c.JSON(http.StatusForbidden, gin.H{errorKey: msgNotYours})
		return
	}
	var body apimodels.LocalGroupRepeaterPatch
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	if body.Slot != 1 && body.Slot != 2 {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadSlot})
		return
	}
	if memberError(c, models.SetLocalGroupMember(db, g.ID, rid, body.Slot, false)) {
		return
	}
	reload(c, rid)
	answer(c, db, a, g)
}

// DELETELocalGroupRepeater takes a repeater out of a group, or says no to
// adding it. The group owner and the repeater owner may.
func DELETELocalGroupRepeater(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	g, rid, owner, ok := memberRequest(c, db)
	if !ok {
		return
	}
	runs, err := a.runs(db, rid, owner)
	if err != nil {
		fail(c, err, "Error finding the co-sysops of a repeater")
		return
	}
	if !a.manages(g) && !runs {
		c.JSON(http.StatusForbidden, gin.H{errorKey: msgNotYours})
		return
	}
	if memberError(c, models.RemoveLocalGroupMember(db, g.ID, rid)) {
		return
	}
	reload(c, rid)
	c.JSON(http.StatusOK, gin.H{messageKey: "Repeater taken out of the local group"})
}

// POSTLocalGroupRepeaterAccept says yes to adding a repeater to a group.
// Only the repeater owner and an admin may.
func POSTLocalGroupRepeaterAccept(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	g, rid, owner, ok := memberRequest(c, db)
	if !ok {
		return
	}
	runs, err := a.runs(db, rid, owner)
	if err != nil {
		fail(c, err, "Error finding the co-sysops of a repeater")
		return
	}
	if !a.admin && !runs {
		c.JSON(http.StatusForbidden, gin.H{errorKey: msgNotRepeaterMine})
		return
	}
	if memberError(c, models.SetLocalGroupMember(db, g.ID, rid, 0, true)) {
		return
	}
	reload(c, rid)
	answer(c, db, a, g)
}

// GETRepeaterLocalGroups answers the local groups that a repeater is in,
// and to its owner and admins also those it is asked to join.
func GETRepeaterLocalGroups(c *gin.Context) {
	db, a, ok := session(c)
	if !ok {
		return
	}
	rid, ok := idParam(c, "id")
	if !ok {
		return
	}
	owner, exists, err := ownerOf(db, rid)
	if err != nil {
		fail(c, err, "Error finding a repeater")
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoRepeater})
		return
	}
	rows, err := models.LocalGroupsOfRepeater(db, rid)
	if err != nil {
		fail(c, err, "Error listing the local groups of a repeater")
		return
	}
	runs, err := a.runs(db, rid, owner)
	if err != nil {
		fail(c, err, "Error finding the co-sysops of a repeater")
		return
	}
	out := []apimodels.RepeaterLocalGroup{}
	for _, r := range rows {
		if r.State != models.LocalMemberIn && !a.admin && !runs {
			continue
		}
		g, err := models.FindLocalGroup(db, r.LocalGroupID)
		if err != nil {
			fail(c, err, "Error finding a local group")
			return
		}
		ids, err := models.LocalGroupRepeaterIDs(db, g.ID)
		if err != nil {
			fail(c, err, "Error listing the repeaters of a local group")
			return
		}
		out = append(out, apimodels.RepeaterLocalGroup{
			ID: g.ID, Name: g.Name, Talkgroup: g.Talkgroup, Slot: r.Slot, State: r.State,
			Owner: userRef(db, g.OwnerID), AskedBy: userRef(db, r.AskedByID), Repeaters: len(ids),
		})
	}
	c.JSON(http.StatusOK, out)
}

func managed(c *gin.Context, db *gorm.DB, a actor) (models.LocalGroup, bool) {
	g, ok := group(c, db)
	if !ok {
		return g, false
	}
	if !a.manages(g) {
		c.JSON(http.StatusForbidden, gin.H{errorKey: msgNotYours})
		return g, false
	}
	return g, true
}

func answer(c *gin.Context, db *gorm.DB, a actor, g models.LocalGroup) {
	v, err := view(db, a, g, true)
	if err != nil {
		fail(c, err, "Error showing a local group")
		return
	}
	c.JSON(http.StatusOK, v)
}

func userRef(db *gorm.DB, id uint) models.UserRef {
	ref := models.UserRef{ID: id}
	if id == 0 {
		return ref
	}
	var callsigns []string
	if err := db.Model(&models.User{}).Where("id = ?", id).Pluck("callsign", &callsigns).Error; err == nil && len(callsigns) > 0 {
		ref.Callsign = callsigns[0]
	}
	return ref
}

// view shows the group g to a, with its repeaters when full is set.
func view(db *gorm.DB, a actor, g models.LocalGroup, full bool) (apimodels.LocalGroup, error) {
	members, err := models.LocalGroupMembers(db, g.ID)
	if err != nil {
		return apimodels.LocalGroup{}, err //nolint:wrapcheck // LocalGroupMembers wraps its errors
	}
	v := apimodels.LocalGroup{
		ID: g.ID, Name: g.Name, Talkgroup: g.Talkgroup, Owner: userRef(db, g.OwnerID),
		Mine: g.OwnerID == a.user.ID, CanManage: a.manages(g), Repeaters: []apimodels.LocalGroupRepeater{},
	}
	for _, m := range members {
		if m.State == models.LocalMemberIn {
			v.Members++
		}
		var rpt models.Repeater
		if err := db.Select("id", "callsign", "owner_id").First(&rpt, m.RepeaterID).Error; err != nil {
			return apimodels.LocalGroup{}, err //nolint:wrapcheck // the caller logs it
		}
		runs, err := a.runs(db, rpt.ID, rpt.OwnerID)
		if err != nil {
			return apimodels.LocalGroup{}, err
		}
		if runs {
			v.Involved = true
		}
		if full {
			v.Repeaters = append(v.Repeaters, apimodels.LocalGroupRepeater{
				ID: rpt.ID, Callsign: rpt.Callsign, Owner: userRef(db, rpt.OwnerID), Slot: m.Slot, State: m.State,
				Mine: runs,
			})
		}
	}
	return v, nil
}
