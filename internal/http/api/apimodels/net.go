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

package apimodels

import (
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// NetStartPost is the request body for starting an ad-hoc net.
type NetStartPost struct {
	TalkgroupID     uint   `json:"talkgroup_id" binding:"required"`
	Description     string `json:"description"`
	DurationMinutes *uint  `json:"duration_minutes,omitempty"`
}

// NetPatch is the request body for updating a net (admin only).
type NetPatch struct {
	Showcase *bool `json:"showcase,omitempty"`
}

// NetResponse is the API response for a net session.
type NetResponse struct {
	ID              uint                    `json:"id"`
	TalkgroupID     uint                    `json:"talkgroup_id"`
	Talkgroup       WSCallResponseTalkgroup `json:"talkgroup"`
	StartedByUser   WSCallResponseUser      `json:"started_by_user"`
	ScheduledNetID  *uint                   `json:"scheduled_net_id,omitempty"`
	StartTime       time.Time               `json:"start_time"`
	EndTime         *time.Time              `json:"end_time,omitempty"`
	DurationMinutes *uint                   `json:"duration_minutes,omitempty"`
	Description     string                  `json:"description"`
	Active          bool                    `json:"active"`
	Showcase        bool                    `json:"showcase"`
	CheckInCount    int                     `json:"check_in_count"`
}

// NewNetResponseFromNet converts a models.Net into a NetResponse.
func NewNetResponseFromNet(net *models.Net, checkInCount int) NetResponse {
	return NetResponse{
		ID:          net.ID,
		TalkgroupID: net.TalkgroupID,
		Talkgroup: WSCallResponseTalkgroup{
			ID:          net.Talkgroup.ID,
			Name:        net.Talkgroup.Name,
			Description: net.Talkgroup.Description,
		},
		StartedByUser: WSCallResponseUser{
			ID:       net.StartedByUser.ID,
			Callsign: net.StartedByUser.Callsign,
		},
		ScheduledNetID:  net.ScheduledNetID,
		StartTime:       net.StartTime,
		EndTime:         net.EndTime,
		DurationMinutes: net.DurationMinutes,
		Description:     net.Description,
		Active:          net.Active,
		Showcase:        net.Showcase,
		CheckInCount:    checkInCount,
	}
}

// NetCheckInResponse represents a single check-in (call) during a net.
type NetCheckInResponse struct {
	CallID     uint               `json:"call_id"`
	User       WSCallResponseUser `json:"user"`
	RepeaterID uint               `json:"repeater_id"`
	StartTime  time.Time          `json:"start_time"`
}

// NewNetCheckInResponseFromCall builds a NetCheckInResponse from a models.Call.
func NewNetCheckInResponseFromCall(call *models.Call) NetCheckInResponse {
	return NetCheckInResponse{
		CallID: call.ID,
		User: WSCallResponseUser{
			ID:       call.User.ID,
			Callsign: call.User.Callsign,
		},
		RepeaterID: call.RepeaterID,
		StartTime:  call.StartTime,
	}
}

// ScheduledNetPost is the request body for creating a scheduled net.
type ScheduledNetPost struct {
	TalkgroupID     uint   `json:"talkgroup_id" binding:"required"`
	Name            string `json:"name" binding:"required"`
	Description     string `json:"description"`
	DayOfWeek       int    `json:"day_of_week" binding:"min=0,max=6"`
	TimeOfDay       string `json:"time_of_day" binding:"required"`
	Timezone        string `json:"timezone" binding:"required"`
	DurationMinutes *uint  `json:"duration_minutes,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	Showcase        *bool  `json:"showcase,omitempty"`
}

// ScheduledNetPatch is the request body for updating a scheduled net.
type ScheduledNetPatch struct {
	Name            *string `json:"name,omitempty"`
	Description     *string `json:"description,omitempty"`
	DayOfWeek       *int    `json:"day_of_week,omitempty"`
	TimeOfDay       *string `json:"time_of_day,omitempty"`
	Timezone        *string `json:"timezone,omitempty"`
	DurationMinutes *uint   `json:"duration_minutes,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	Showcase        *bool   `json:"showcase,omitempty"`
}

// ScheduledNetResponse is the API response for a scheduled net.
type ScheduledNetResponse struct {
	ID              uint                    `json:"id"`
	TalkgroupID     uint                    `json:"talkgroup_id"`
	Talkgroup       WSCallResponseTalkgroup `json:"talkgroup"`
	CreatedByUser   WSCallResponseUser      `json:"created_by_user"`
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	CronExpression  string                  `json:"cron_expression"`
	DayOfWeek       int                     `json:"day_of_week"`
	TimeOfDay       string                  `json:"time_of_day"`
	Timezone        string                  `json:"timezone"`
	DurationMinutes *uint                   `json:"duration_minutes,omitempty"`
	Enabled         bool                    `json:"enabled"`
	Showcase        bool                    `json:"showcase"`
	NextRun         *time.Time              `json:"next_run,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
}

// NewScheduledNetResponseFromScheduledNet converts a models.ScheduledNet into a ScheduledNetResponse.
func NewScheduledNetResponseFromScheduledNet(sn *models.ScheduledNet) ScheduledNetResponse {
	return ScheduledNetResponse{
		ID:          sn.ID,
		TalkgroupID: sn.TalkgroupID,
		Talkgroup: WSCallResponseTalkgroup{
			ID:          sn.Talkgroup.ID,
			Name:        sn.Talkgroup.Name,
			Description: sn.Talkgroup.Description,
		},
		CreatedByUser: WSCallResponseUser{
			ID:       sn.CreatedByUser.ID,
			Callsign: sn.CreatedByUser.Callsign,
		},
		Name:            sn.Name,
		Description:     sn.Description,
		CronExpression:  sn.CronExpression,
		DayOfWeek:       sn.DayOfWeek,
		TimeOfDay:       sn.TimeOfDay,
		Timezone:        sn.Timezone,
		DurationMinutes: sn.DurationMinutes,
		Enabled:         sn.Enabled,
		Showcase:        sn.Showcase,
		NextRun:         sn.NextRun,
		CreatedAt:       sn.CreatedAt,
	}
}

// WSNetEventResponse is sent over WebSocket when a net starts or stops.
type WSNetEventResponse struct {
	NetID       uint                    `json:"net_id"`
	TalkgroupID uint                    `json:"talkgroup_id"`
	Talkgroup   WSCallResponseTalkgroup `json:"talkgroup"`
	Event       string                  `json:"event"` // "started" or "stopped"
	Active      bool                    `json:"active"`
	StartTime   time.Time               `json:"start_time"`
	EndTime     *time.Time              `json:"end_time,omitempty"`
}

// WSNetCheckInResponse is sent over WebSocket when a check-in occurs during an active net.
type WSNetCheckInResponse struct {
	NetID     uint               `json:"net_id"`
	CallID    uint               `json:"call_id"`
	User      WSCallResponseUser `json:"user"`
	StartTime time.Time          `json:"start_time"`
	Duration  time.Duration      `json:"duration"`
}
