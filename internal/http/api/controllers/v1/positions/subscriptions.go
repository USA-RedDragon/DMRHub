// SPDX-License-Identifier: AGPL-3.0-or-later

package positions

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type subscriptionView struct {
	State          string                  `json:"state"`
	Available      bool                    `json:"available"`
	Online         bool                    `json:"online"`
	RequestID      string                  `json:"request_id,omitempty"`
	Service        uint32                  `json:"service_id,omitempty"`
	Interval       uint32                  `json:"interval_seconds,omitempty"`
	SentAt         int64                   `json:"sent_at_ms,omitempty"`
	AcknowledgedAt int64                   `json:"acknowledged_at_ms,omitempty"`
	ReportedAt     int64                   `json:"reported_at_ms,omitempty"`
	Detail         string                  `json:"detail,omitempty"`
	Command        *models.LocationCommand `json:"command,omitempty"`
	RetryAt        int64                   `json:"retry_at_ms,omitempty"`
}

func subscriptionRadio(c *gin.Context) (*gorm.DB, models.PositionSetting, bool) {
	db, ok := utils.GetDB(c)
	if !ok {
		return nil, models.PositionSetting{}, false
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return nil, models.PositionSetting{}, false
	}
	radio, ok := radioParam(c)
	if !ok {
		return nil, models.PositionSetting{}, false
	}
	owner, owned, err := models.RadioIDOwner(db, radio)
	if err != nil {
		failed(c, "cannot check radio ownership", err)
		return nil, models.PositionSetting{}, false
	}
	if !owned || owner != user.ID {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "You do not own this radio ID"})
		return nil, models.PositionSetting{}, false
	}
	s, _, err := models.FindPositionSetting(db, radio)
	if err != nil {
		failed(c, "cannot read position settings", err)
		return nil, models.PositionSetting{}, false
	}
	if s.UserID != user.ID {
		s.Tracking = false
	}
	s.UserID = user.ID
	return db, s, true
}

func locatorAvailability(db *gorm.DB, now time.Time) (enabled, online bool, err error) {
	var rows []models.Plugin
	err = db.Select("session", "last_seen_at").Where("builtin = ? AND enabled = ?", "locator", true).Limit(1).Find(&rows).Error
	return len(rows) > 0, len(rows) > 0 && rows[0].Online(now), err
}

func subscriptionStatus(s *pluginv1.LocationStatus, now time.Time) subscriptionView {
	out := subscriptionView{
		State: s.GetState(), RequestID: hex.EncodeToString(s.GetRequestId()), Service: s.GetService(), Interval: s.GetIntervalSeconds(),
		SentAt: s.GetSentAt(), AcknowledgedAt: s.GetAcknowledgedAt(), ReportedAt: s.GetReportedAt(), Detail: s.GetDetail(),
	}
	switch out.State {
	case "accepted", "reporting":
		last := out.ReportedAt
		if last == 0 {
			last = out.AcknowledgedAt
		}
		if now.Sub(time.UnixMilli(last)) >= 3*time.Duration(max(out.Interval, 1))*time.Second {
			out.State, out.Detail = "stale", "No recent periodic location report"
		}
	case "pending", "stopping":
		if now.Sub(time.UnixMilli(out.SentAt)) >= time.Minute {
			out.State, out.Detail = "failed", "No LRRP answer received"
		}
	case "stop_failed":
		out.State = "failed"
	}
	return out
}

func GETLocationSubscription(c *gin.Context) {
	db, setting, ok := subscriptionRadio(c)
	if !ok {
		return
	}
	cfg, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	now := time.Now()
	available, online, err := locatorAvailability(db, now)
	if err != nil {
		failed(c, "cannot check Locator availability", err)
		return
	}
	out := subscriptionView{State: "off", Available: cfg.DMR.Plugins.InProcess && available, Online: online}
	if !setting.Tracking {
		c.JSON(http.StatusOK, out)
		return
	}
	out.State = "configured"
	var rows []models.LocationSubscription
	if err := db.Where("radio_id = ? AND user_id = ?", setting.RadioID, setting.UserID).Limit(1).Find(&rows).Error; err != nil {
		failed(c, "cannot read location subscription", err)
		return
	}
	if len(rows) > 0 {
		var status pluginv1.LocationStatus
		if err := json.Unmarshal(rows[0].Snapshot, &status); err != nil {
			failed(c, "cannot decode location subscription", err)
			return
		}
		out = subscriptionStatus(&status, now)
		out.Available, out.Online = cfg.DMR.Plugins.InProcess && available, online
	}
	var commands []models.LocationCommand
	if err := db.Where("radio_id = ? AND user_id = ?", setting.RadioID, setting.UserID).Limit(1).Find(&commands).Error; err != nil {
		failed(c, "cannot read manual location request", err)
		return
	}
	if len(commands) > 0 {
		cmd := &commands[0]
		if now.Before(cmd.ExpiresAt) {
			out.RetryAt = cmd.ExpiresAt.UnixMilli()
		}
		if !now.Before(cmd.ExpiresAt) {
			switch cmd.State {
			case "queued":
				cmd.State, cmd.Detail = "failed", "Request expired before it could be sent"
			case "pending":
				cmd.State, cmd.Detail = "failed", "No LRRP answer received"
			}
		}
		out.Command = cmd
		if out.State == "configured" && cmd.Action == "subscribe" {
			switch cmd.State {
			case "queued", "pending", "failed":
				out.State, out.Detail = cmd.State, cmd.Detail
			}
		}
	}
	c.JSON(http.StatusOK, out)
}

func POSTLocationCommand(c *gin.Context) {
	db, setting, ok := subscriptionRadio(c)
	if !ok {
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Action != "subscribe" && req.Action != "once" && req.Action != "unsubscribe") {
		badRequest(c, "Action must be subscribe, once or unsubscribe")
		return
	}
	if !setting.Tracking {
		c.JSON(http.StatusConflict, gin.H{errorKey: "Turn on position tracking before requesting location reports"})
		return
	}
	cfg, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	now := time.Now().UTC()
	available, _, err := locatorAvailability(db, now)
	if err != nil {
		failed(c, "cannot check Locator availability", err)
		return
	}
	if !cfg.DMR.Plugins.InProcess || !available {
		c.JSON(http.StatusServiceUnavailable, gin.H{errorKey: "Enable the built-in Location server to send requests"})
		return
	}
	command, queued, err := models.QueueLocationCommand(db, setting.RadioID, setting.UserID, req.Action, now)
	if err != nil {
		failed(c, "cannot queue location request", err)
		return
	}
	if !queued {
		c.JSON(http.StatusTooManyRequests, gin.H{errorKey: "Wait for the current request to finish and try again shortly"})
		return
	}
	c.JSON(http.StatusAccepted, command)
}
