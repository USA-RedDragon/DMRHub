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
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/netscheduler"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GETScheduledNets lists scheduled nets with optional filtering by talkgroup_id.
func GETScheduledNets(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}

	tgIDStr := c.Query("talkgroup_id")

	var scheduledNets []models.ScheduledNet
	var count int
	var err error

	if tgIDStr != "" {
		tgID, parseErr := strconv.ParseUint(tgIDStr, 10, 32)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid talkgroup_id"})
			return
		}
		scheduledNets, err = models.FindScheduledNetsForTalkgroup(db, uint(tgID))
		if err != nil {
			slog.Error("Failed to list scheduled nets for talkgroup", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
			return
		}
		count, err = models.CountScheduledNetsForTalkgroup(cDb, uint(tgID))
	} else {
		scheduledNets, err = models.ListScheduledNets(db)
		if err != nil {
			slog.Error("Failed to list scheduled nets", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
			return
		}
		count, err = models.CountScheduledNets(cDb)
	}
	if err != nil {
		slog.Error("Failed to count scheduled nets", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	resp := make([]apimodels.ScheduledNetResponse, 0, len(scheduledNets))
	for i := range scheduledNets {
		resp = append(resp, apimodels.NewScheduledNetResponseFromScheduledNet(&scheduledNets[i]))
	}
	c.JSON(http.StatusOK, gin.H{"scheduled_nets": resp, "total": count})
}

// GETScheduledNet returns a single scheduled net by ID.
func GETScheduledNet(c *gin.Context) {
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

	sn, err := models.FindScheduledNetByID(db, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Scheduled net not found"})
			return
		}
		slog.Error("Failed to find scheduled net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	c.JSON(http.StatusOK, apimodels.NewScheduledNetResponseFromScheduledNet(&sn))
}

// POSTScheduledNet creates a new scheduled net.
func POSTScheduledNet(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	var req apimodels.ScheduledNetPost
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate name length.
	if len(req.Name) == 0 || len(req.Name) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name must be between 1 and 100 characters"})
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

	cronExpr, err := models.GenerateCronExpression(req.DayOfWeek, req.TimeOfDay)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session := sessions.Default(c)
	userID, _ := session.Get("user_id").(uint)

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	showcase := false
	if req.Showcase != nil {
		showcase = *req.Showcase
	}

	sn := models.ScheduledNet{
		TalkgroupID:     req.TalkgroupID,
		CreatedByUserID: userID,
		Name:            req.Name,
		Description:     req.Description,
		CronExpression:  cronExpr,
		DayOfWeek:       req.DayOfWeek,
		TimeOfDay:       req.TimeOfDay,
		Timezone:        req.Timezone,
		DurationMinutes: req.DurationMinutes,
		Enabled:         enabled,
		Showcase:        showcase,
	}
	if err := models.CreateScheduledNet(db, &sn); err != nil {
		slog.Error("Failed to create scheduled net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Register with the scheduler if enabled.
	if enabled {
		if ns, nsOK := c.MustGet("NetScheduler").(*netscheduler.NetScheduler); nsOK && ns != nil {
			if err := ns.RegisterScheduledNet(c.Request.Context(), &sn); err != nil {
				slog.Error("Failed to register scheduled net with scheduler", "error", err)
			}
		}
	}

	// Reload with associations.
	sn, err = models.FindScheduledNetByID(db, sn.ID)
	if err != nil {
		slog.Error("Failed to reload scheduled net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	c.JSON(http.StatusCreated, apimodels.NewScheduledNetResponseFromScheduledNet(&sn))
}

// PATCHScheduledNet updates a scheduled net.
func PATCHScheduledNet(c *gin.Context) {
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

	sn, err := models.FindScheduledNetByID(db, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Scheduled net not found"})
			return
		}
		slog.Error("Failed to find scheduled net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	var req apimodels.ScheduledNetPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	scheduleChanged := false
	if req.Name != nil {
		if len(*req.Name) == 0 || len(*req.Name) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Name must be between 1 and 100 characters"})
			return
		}
		sn.Name = *req.Name
	}
	if req.Description != nil {
		sn.Description = *req.Description
	}
	if req.DayOfWeek != nil {
		sn.DayOfWeek = *req.DayOfWeek
		scheduleChanged = true
	}
	if req.TimeOfDay != nil {
		sn.TimeOfDay = *req.TimeOfDay
		scheduleChanged = true
	}
	if req.Timezone != nil {
		sn.Timezone = *req.Timezone
		scheduleChanged = true
	}
	if req.DurationMinutes != nil {
		sn.DurationMinutes = req.DurationMinutes
	}
	if req.Enabled != nil {
		sn.Enabled = *req.Enabled
	}
	if req.Showcase != nil {
		sn.Showcase = *req.Showcase
	}

	// Regenerate cron expression if schedule fields changed.
	if scheduleChanged {
		cronExpr, cronErr := models.GenerateCronExpression(sn.DayOfWeek, sn.TimeOfDay)
		if cronErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": cronErr.Error()})
			return
		}
		sn.CronExpression = cronExpr
	}

	if err := models.UpdateScheduledNet(db, &sn); err != nil {
		slog.Error("Failed to update scheduled net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Re-register with the scheduler.
	if ns, nsOK := c.MustGet("NetScheduler").(*netscheduler.NetScheduler); nsOK && ns != nil {
		if sn.Enabled {
			if err := ns.RegisterScheduledNet(c.Request.Context(), &sn); err != nil {
				slog.Error("Failed to re-register scheduled net with scheduler", "error", err)
			}
		} else {
			ns.UnregisterScheduledNet(sn.ID)
		}
	}

	// Reload with associations.
	sn, err = models.FindScheduledNetByID(db, sn.ID)
	if err != nil {
		slog.Error("Failed to reload scheduled net after update", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	c.JSON(http.StatusOK, apimodels.NewScheduledNetResponseFromScheduledNet(&sn))
}

// DELETEScheduledNet soft-deletes a scheduled net.
func DELETEScheduledNet(c *gin.Context) {
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

	// Unregister from scheduler before deleting.
	if ns, nsOK := c.MustGet("NetScheduler").(*netscheduler.NetScheduler); nsOK && ns != nil {
		ns.UnregisterScheduledNet(uint(id))
	}

	if err := models.DeleteScheduledNet(db, uint(id)); err != nil {
		slog.Error("Failed to delete scheduled net", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Scheduled net deleted"})
}
