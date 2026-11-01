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
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/connlog"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const msgNoCall = "That call is not on the air on this repeater"

func slotNumber(ts2 bool) int {
	if ts2 {
		return 2
	}
	return 1
}

func onAir(rpt *models.Repeater, call *models.Call) (apimodels.OnAirCall, bool) {
	out := apimodels.OnAirCall{CallID: call.ID, SourceID: call.SourceID, Callsign: call.User.Callsign, Destination: call.DestinationID, GroupCall: call.GroupCall, StartTime: call.StartTime}
	switch {
	case call.RepeaterID != nil && *call.RepeaterID == rpt.ID:
		out.Direction, out.Slot = "from", slotNumber(call.TimeSlot)
	case call.IsToRepeater && call.ToRepeaterID != nil && *call.ToRepeaterID == rpt.ID:
		out.Direction, out.Slot = "to", slotNumber(call.TimeSlot)
	case call.IsToTalkgroup:
		want, slot := rpt.WantRXCall(*call)
		if !want {
			return out, false
		}
		out.Direction, out.Slot = "to", slotNumber(slot)
	default:
		return out, false
	}
	return out, true
}

func activeVoiceCalls(db *gorm.DB) ([]models.Call, error) {
	var calls []models.Call
	err := db.Preload("User").Where("active = ? AND kind = ?", true, models.CallKindVoice).Order("start_time ASC").Find(&calls).Error
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	return calls, nil
}

func sysopRepeater(c *gin.Context, db *gorm.DB) (models.Repeater, bool) {
	rpt, ok := cosysopRepeater(c, db)
	if !ok {
		return rpt, false
	}
	full, err := models.FindRepeaterByID(db, rpt.ID)
	if err != nil {
		slog.Error("Error finding a repeater", "repeaterID", rpt.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingRepeater})
		return rpt, false
	}
	return full, true
}

// GETRepeaterOnAir lists the voice calls that the repeater carries now, in
// both directions.
func GETRepeaterOnAir(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	rpt, ok := sysopRepeater(c, db)
	if !ok {
		return
	}
	calls, err := activeVoiceCalls(db)
	if err != nil {
		slog.Error("Error listing the calls on the air", "repeaterID", rpt.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	out := make([]apimodels.OnAirCall, 0, 2)
	for i := range calls {
		if a, ok := onAir(&rpt, &calls[i]); ok {
			out = append(out, a)
		}
	}
	c.JSON(http.StatusOK, gin.H{"calls": out})
}

// POSTRepeaterDropCall drops a call on the air on the repeater: every
// replica stops carrying it to or from the repeater until it ends, and the
// radios get its terminator.
func POSTRepeaterDropCall(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	rpt, ok := sysopRepeater(c, db)
	if !ok {
		return
	}
	var post apimodels.DropCallPost
	if err := c.ShouldBindJSON(&post); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return
	}
	var call models.Call
	err := db.Preload("User").Where("id = ? AND active = ? AND kind = ?", post.CallID, true, models.CallKindVoice).Take(&call).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoCall})
		return
	}
	if err != nil {
		slog.Error("Error finding a call to drop", "callID", post.CallID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	a, ok := onAir(&rpt, &call)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNoCall})
		return
	}
	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if !ok || dmrHub == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{errorKey: msgTryAgainLater})
		return
	}
	dmrHub.DropCall(rpt.ID, a.Slot == 2, call.StreamID, call.SourceID, call.DestinationID)
	slog.Info("A sysop dropped a call", "repeaterID", rpt.ID, "callID", call.ID, "slot", a.Slot)
	c.JSON(http.StatusOK, a)
}

// POSTRepeaterReset ends the sessions of the repeater on every replica,
// so that it logs in again, and adds the reset to its connection log.
func POSTRepeaterReset(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := sessionUser(c, db)
	if !ok {
		return
	}
	rpt, ok := cosysopRepeater(c, db)
	if !ok {
		return
	}
	dmrHub, ok := c.MustGet("Hub").(*hub.Hub)
	if !ok || dmrHub == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{errorKey: msgTryAgainLater})
		return
	}
	if _, err := models.NoteRepeaterEvent(db, models.RepeaterEvent{RepeaterID: rpt.ID, Kind: connlog.Reset, Detail: user.Callsign, LastAt: time.Now()}, 1); err != nil {
		slog.Error("Error logging a reset", "repeaterID", rpt.ID, "error", err)
	}
	dmrHub.RepeaterReset(rpt.ID)
	slog.Info("A sysop reset a repeater", "repeaterID", rpt.ID, "by", user.ID)
	c.JSON(http.StatusOK, gin.H{messageKey: "Connection reset"})
}
