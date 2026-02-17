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

package nets

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/netscheduler"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GETNets lists nets with optional filtering by talkgroup_id and active status.
func GETNets(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}

	const queryTrue = "true"

	tgIDStr := c.Query("talkgroup_id")
	activeStr := c.Query("active")
	showcaseStr := c.Query("showcase")

	var nets []models.Net
	var count int
	var err error

	switch {
	case showcaseStr == queryTrue:
		nets, err = models.ListShowcaseNets(db)
		if err != nil {
			slog.Error("Failed to list showcase nets", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
			return
		}
		count = len(nets)
	case tgIDStr != "":
		tgID, parseErr := strconv.ParseUint(tgIDStr, 10, 32)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup_id"})
			return
		}
		if activeStr == queryTrue {
			net, findErr := models.FindActiveNetForTalkgroup(cDb, uint(tgID))
			if findErr != nil {
				if errors.Is(findErr, gorm.ErrRecordNotFound) {
					c.JSON(http.StatusOK, gin.H{"nets": []any{}, "total": 0})
					return
				}
				slog.Error("Failed to find active net", "error", findErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
				return
			}
			checkIns := countCheckInsForNet(cDb, &net)
			c.JSON(http.StatusOK, gin.H{
				"nets":  []apimodels.NetResponse{apimodels.NewNetResponseFromNet(&net, checkIns)},
				"total": 1,
			})
			return
		}
		nets, err = models.FindNetsForTalkgroup(db, uint(tgID))
		if err != nil {
			slog.Error("Failed to list nets for talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
			return
		}
		count, err = models.CountNetsForTalkgroup(cDb, uint(tgID))
	case activeStr == queryTrue:
		nets, err = models.ListActiveNets(db)
		if err != nil {
			slog.Error("Failed to list active nets", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
			return
		}
		count, err = models.CountActiveNets(cDb)
	default:
		nets, err = models.ListNets(db)
		if err != nil {
			slog.Error("Failed to list nets", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
			return
		}
		count, err = models.CountNets(cDb)
	}
	if err != nil {
		slog.Error("Failed to count nets", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	resp := make([]apimodels.NetResponse, 0, len(nets))
	for i := range nets {
		checkIns := countCheckInsForNet(cDb, &nets[i])
		resp = append(resp, apimodels.NewNetResponseFromNet(&nets[i], checkIns))
	}
	c.JSON(http.StatusOK, gin.H{"nets": resp, "total": count})
}

// GETNet returns a single net by ID.
func GETNet(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}

	net, err := models.FindNetByID(db, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Net not found"})
			return
		}
		slog.Error("Failed to find net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	checkIns := countCheckInsForNet(db, &net)
	c.JSON(http.StatusOK, apimodels.NewNetResponseFromNet(&net, checkIns))
}

// PATCHNet updates a net (admin-only). Currently supports toggling the showcase flag.
func PATCHNet(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}

	var req apimodels.NetPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Showcase != nil {
		if err := models.UpdateNetShowcase(db, uint(id), *req.Showcase); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "Net not found"})
				return
			}
			slog.Error("Failed to update net showcase", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
			return
		}
	}

	net, err := models.FindNetByID(db, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Net not found"})
			return
		}
		slog.Error("Failed to reload net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	checkIns := countCheckInsForNet(db, &net)
	c.JSON(http.StatusOK, apimodels.NewNetResponseFromNet(&net, checkIns))
}

// POSTNetStart starts a new ad-hoc net on a talkgroup.
func POSTNetStart(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	var req apimodels.NetStartPost
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Verify the talkgroup exists.
	exists, err := models.TalkgroupIDExists(db, req.TalkgroupID)
	if err != nil {
		slog.Error("Failed to check talkgroup existence", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Talkgroup not found"})
		return
	}

	// Check that no active net exists for this talkgroup.
	_, err = models.FindActiveNetForTalkgroup(db, req.TalkgroupID)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "An active net already exists for this talkgroup"})
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Error("Failed to check for active net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	session := sessions.Default(c)
	userID, _ := session.Get("user_id").(uint)

	net := models.Net{
		TalkgroupID:     req.TalkgroupID,
		StartedByUserID: userID,
		StartTime:       time.Now(),
		DurationMinutes: req.DurationMinutes,
		Description:     req.Description,
		Active:          true,
	}
	if err := models.CreateNet(db, &net); err != nil {
		slog.Error("Failed to create net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Reload with associations.
	net, err = models.FindNetByID(db, net.ID)
	if err != nil {
		slog.Error("Failed to reload net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Publish net started event.
	publishNetEvent(c, &net, "started")

	// If a duration is set, schedule auto-close.
	if req.DurationMinutes != nil && *req.DurationMinutes > 0 {
		if ns, nsOK := c.MustGet("NetScheduler").(*netscheduler.NetScheduler); nsOK && ns != nil {
			dur := min(*req.DurationMinutes, uint(math.MaxInt64/int64(time.Minute)))
			ns.ScheduleAutoClose(net.ID, time.Duration(dur)*time.Minute) //nolint:gosec // bounded by min
		}
	}

	c.JSON(http.StatusCreated, apimodels.NewNetResponseFromNet(&net, 0))
}

// POSTNetStop stops an active net.
func POSTNetStop(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}

	if err := models.EndNet(db, uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Active net not found"})
			return
		}
		slog.Error("Failed to end net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Cancel auto-close timer if one is running.
	if ns, nsOK := c.MustGet("NetScheduler").(*netscheduler.NetScheduler); nsOK && ns != nil {
		ns.CancelAutoClose(uint(id))
	}

	// Reload for response.
	net, err := models.FindNetByID(db, uint(id))
	if err != nil {
		slog.Error("Failed to reload net after stop", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Publish net stopped event.
	publishNetEvent(c, &net, "stopped")
	checkIns := countCheckInsForNet(db, &net)
	c.JSON(http.StatusOK, apimodels.NewNetResponseFromNet(&net, checkIns))
}

// GETNetCheckIns returns the check-in list for a net.
func GETNetCheckIns(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}

	net, err := models.FindNetByID(cDb, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Net not found"})
			return
		}
		slog.Error("Failed to find net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	endTime := time.Now()
	if net.EndTime != nil {
		endTime = *net.EndTime
	}

	calls, err := models.FindTalkgroupCallsInTimeRange(db, net.TalkgroupID, net.StartTime, endTime)
	if err != nil {
		slog.Error("Failed to find check-in calls", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	count, err := models.CountTalkgroupCallsInTimeRange(cDb, net.TalkgroupID, net.StartTime, endTime)
	if err != nil {
		slog.Error("Failed to count check-in calls", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	resp := make([]apimodels.NetCheckInResponse, 0, len(calls))
	for i := range calls {
		resp = append(resp, apimodels.NewNetCheckInResponseFromCall(&calls[i]))
	}
	c.JSON(http.StatusOK, gin.H{"check_ins": resp, "total": count})
}

// GETNetCheckInsExport exports the check-in list as CSV or JSON.
func GETNetCheckInsExport(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}

	net, err := models.FindNetByID(db, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Net not found"})
			return
		}
		slog.Error("Failed to find net for export", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	endTime := time.Now()
	if net.EndTime != nil {
		endTime = *net.EndTime
	}

	calls, err := models.FindTalkgroupCallsInTimeRange(db, net.TalkgroupID, net.StartTime, endTime)
	if err != nil {
		slog.Error("Failed to find check-in calls for export", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	resp := make([]apimodels.NetCheckInResponse, 0, len(calls))
	for i := range calls {
		resp = append(resp, apimodels.NewNetCheckInResponseFromCall(&calls[i]))
	}

	format := c.DefaultQuery("format", "csv")
	filename := fmt.Sprintf("net_%d_checkins", id)

	switch format {
	case "json":
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.json", filename))
		c.JSON(http.StatusOK, resp)
	case "csv":
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		c.Header("Content-Type", "text/csv")
		w := csv.NewWriter(c.Writer)
		_ = w.Write([]string{"Call ID", "User ID", "Callsign", "Start Time", "Duration", "Time Slot", "Loss", "Jitter", "BER", "RSSI"})
		for _, ci := range resp {
			ts := "1"
			if ci.TimeSlot {
				ts = "2"
			}
			_ = w.Write([]string{
				strconv.FormatUint(uint64(ci.CallID), 10),
				strconv.FormatUint(uint64(ci.User.ID), 10),
				ci.User.Callsign,
				ci.StartTime.Format(time.RFC3339),
				ci.Duration.String(),
				ts,
				fmt.Sprintf("%.2f", ci.Loss),
				fmt.Sprintf("%.2f", ci.Jitter),
				fmt.Sprintf("%.2f", ci.BER),
				fmt.Sprintf("%.2f", ci.RSSI),
			})
		}
		w.Flush()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid format, must be csv or json"})
	}
}

// countCheckInsForNet returns the number of calls during a net's time window.
func countCheckInsForNet(db *gorm.DB, net *models.Net) int {
	endTime := time.Now()
	if net.EndTime != nil {
		endTime = *net.EndTime
	}
	count, err := models.CountTalkgroupCallsInTimeRange(db, net.TalkgroupID, net.StartTime, endTime)
	if err != nil {
		slog.Error("Failed to count check-ins for net", "error", err)
		return 0
	}
	return count
}

// publishNetEvent publishes a net start/stop event to pubsub.
func publishNetEvent(c *gin.Context, net *models.Net, event string) {
	ps, ok := c.MustGet("PubSub").(pubsub.PubSub)
	if !ok {
		slog.Error("Failed to get PubSub from context")
		return
	}
	evt := apimodels.WSNetEventResponse{
		NetID:       net.ID,
		TalkgroupID: net.TalkgroupID,
		Talkgroup: apimodels.WSCallResponseTalkgroup{
			ID:          net.Talkgroup.ID,
			Name:        net.Talkgroup.Name,
			Description: net.Talkgroup.Description,
		},
		Event:     event,
		Active:    net.Active,
		StartTime: net.StartTime,
		EndTime:   net.EndTime,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		slog.Error("Failed to marshal net event", "error", err)
		return
	}
	topic := fmt.Sprintf("net:events:%d", net.TalkgroupID)
	if err := ps.Publish(topic, data); err != nil {
		slog.Error("Failed to publish net event", "error", err)
	}
	// Also publish to the general topic for the WebSocket.
	if err := ps.Publish("net:events", data); err != nil {
		slog.Error("Failed to publish net event to general topic", "error", err)
	}
}
