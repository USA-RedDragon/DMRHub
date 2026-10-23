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

package positions

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const hoursPerDay = 24

func intervals(cfg *config.Config) []uint {
	var out []uint
	for _, m := range config.PositionIntervals() {
		if m >= cfg.DMR.Positions.Interval() {
			out = append(out, uint(m)) //nolint:gosec // the intervals are 5 to 60
		}
	}
	return out
}

func retentions(cfg *config.Config) []uint {
	most := uint(cfg.DMR.Positions.Retention()) * hoursPerDay //nolint:gosec // validation keeps it from 1 to 365
	var out []uint
	for _, h := range []uint{hoursPerDay, 7 * hoursPerDay, 30 * hoursPerDay, most} {
		if h <= most && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

func ownRadios(db *gorm.DB, user *models.User) ([]uint, error) {
	ids, err := models.ListUserRadioIDs(db, user.ID)
	if err != nil {
		return nil, fmt.Errorf("could not list the radio IDs of %d: %w", user.ID, err)
	}
	out := []uint{user.ID}
	for _, id := range ids {
		out = append(out, id.RadioID)
	}
	return out, nil
}

// GETPositionRadios returns the position settings of the radio IDs of the
// user, and the choices that the hub allows.
func GETPositionRadios(c *gin.Context) {
	cfg, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	radios, err := ownRadios(db, &user)
	if err != nil {
		failed(c, "cannot list radio IDs", err)
		return
	}
	out := apimodels.PositionRadios{
		Radios: make([]apimodels.PositionRadio, 0, len(radios)), Intervals: intervals(cfg), Retention: retentions(cfg),
		APRS: cfg.DMR.Positions.APRSIS.Callsign != "", Callsign: user.Callsign,
	}
	for _, id := range radios {
		s, _, err := models.FindPositionSetting(db, id)
		if err != nil {
			failed(c, "cannot read a position setting", err)
			return
		}
		out.Radios = append(out.Radios, apimodels.NewPositionRadio(&s))
	}
	c.JSON(http.StatusOK, out)
}

func badRequest(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{errorKey: msg})
}

func validPut(c *gin.Context, cfg *config.Config, req *apimodels.PositionRadioPut) bool {
	switch {
	case !slices.Contains(intervals(cfg), req.IntervalMinutes):
		badRequest(c, "Pick a report interval that this hub allows")
	case !slices.Contains(retentions(cfg), req.RetentionHours):
		badRequest(c, "Pick a time to keep positions that this hub allows")
	case req.APRS && cfg.DMR.Positions.APRSIS.Callsign == "":
		badRequest(c, "This hub does not send positions to APRS-IS")
	case req.APRS && !*req.Tracking:
		badRequest(c, "Turn on position tracking to send to APRS-IS")
	case !aprs.ValidSSID(req.APRSSSID):
		badRequest(c, "Pick an SSID from 1 to 15, or none")
	case req.APRSSymbol != "" && !validSymbol(req.APRSSymbol):
		badRequest(c, "Pick one of the symbols")
	case !aprs.ValidComment(req.APRSComment):
		badRequest(c, "The comment must be at most 43 plain characters, without | or ~")
	default:
		return true
	}
	return false
}

func validSymbol(name string) bool {
	_, ok := aprs.LookupSymbol(name)
	return ok
}

// PUTPositionRadio sets the position setting of a radio ID of the user.
// Turning tracking off stops the open location request of the radio and
// deletes its positions; a new interval replaces the open request.
func PUTPositionRadio(c *gin.Context) {
	cfg, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	radio, ok := radioParam(c)
	if !ok {
		return
	}
	owner, owned, err := models.RadioIDOwner(db, radio)
	if err != nil {
		failed(c, "cannot find the owner of a radio ID", err)
		return
	}
	if !owned || owner != user.ID {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "You do not own this radio ID"})
		return
	}
	var req apimodels.PositionRadioPut
	if err := c.ShouldBindJSON(&req); err != nil || req.Tracking == nil {
		badRequest(c, "Send tracking, interval_minutes and retention_hours")
		return
	}
	if !validPut(c, cfg, &req) {
		return
	}
	before, _, err := models.FindPositionSetting(db, radio)
	if err != nil {
		failed(c, "cannot read a position setting", err)
		return
	}
	if err := models.SetPositionTracking(db, user.ID, radio, *req.Tracking, time.Now()); err != nil {
		failed(c, "cannot set position tracking", err)
		return
	}
	err = models.SetPositionOptions(db, user.ID, radio, models.PositionOptions{
		Interval:  time.Duration(req.IntervalMinutes) * time.Minute, //nolint:gosec // validPut allows at most 60
		Retention: time.Duration(req.RetentionHours) * time.Hour,    //nolint:gosec // validPut allows at most 365 days
		APRS:      req.APRS, APRSSSID: req.APRSSSID, APRSSymbol: req.APRSSymbol, APRSComment: req.APRSComment,
	})
	if err != nil {
		failed(c, "cannot set position options", err)
		return
	}
	if dmrHub, ok := c.MustGet("Hub").(*hub.Hub); ok && dmrHub != nil {
		switch {
		case before.Tracking && !*req.Tracking:
			dmrHub.StopTracking(radio)
		case before.Tracking && before.IntervalSeconds != req.IntervalMinutes*60:
			dmrHub.Retrack(radio)
		}
	}
	after, _, err := models.FindPositionSetting(db, radio)
	if err != nil {
		failed(c, "cannot read a position setting", err)
		return
	}
	c.JSON(http.StatusOK, apimodels.NewPositionRadio(&after))
}
