// SPDX-License-Identifier: AGPL-3.0-or-later

package plugins

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (w *watcher) restoreLocations() {
	var snapshots []models.LocationSubscription
	err := w.s.db.Model(&models.LocationSubscription{}).
		Joins("JOIN position_settings ON position_settings.radio_id = location_subscriptions.radio_id AND position_settings.user_id = location_subscriptions.user_id").
		Where("position_settings.tracking = ?", true).Find(&snapshots).Error
	if err != nil {
		slog.Error("Locator: cannot restore subscription evidence", "error", err)
		return
	}
	for _, row := range snapshots {
		var status pluginv1.LocationStatus
		if json.Unmarshal(row.Snapshot, &status) == nil && inRanges(w.p.Ranges, uint(status.GetService())) {
			w.sendLocation(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_LocationStatus{LocationStatus: &status}})
		}
	}
}

func (w *watcher) dispatchLocations() {
	if w.tracking == nil {
		return
	}
	now := w.s.clock.Now()
	var commands []models.LocationCommand
	err := w.s.db.Model(&models.LocationCommand{}).
		Joins("JOIN position_settings ON position_settings.radio_id = location_commands.radio_id AND position_settings.user_id = location_commands.user_id").
		Where("position_settings.tracking = ? AND location_commands.state = ? AND location_commands.session = '' AND location_commands.expires_at > ?", true, "queued", now).
		Order("location_commands.created_at").Limit(32).
		Find(&commands).Error
	if err != nil {
		slog.Error("Locator: cannot list manual requests", "error", err)
		return
	}
	for _, cmd := range commands {
		owner, owned, err := models.RadioIDOwner(w.s.db, cmd.RadioID)
		if err != nil || !owned || owner != cmd.UserID {
			continue
		}
		active := w.s.db.Model(&models.Plugin{}).Select("id").Where("id = ? AND session = ? AND enabled = ?", w.p.ID, w.session, true)
		claimed := w.s.db.Model(&models.LocationCommand{}).
			Where("radio_id = ? AND id = ? AND session = '' AND EXISTS (?)", cmd.RadioID, cmd.ID, active).
			Update("session", w.session)
		if claimed.Error != nil {
			slog.Error("Locator: cannot claim manual request", "radio", cmd.RadioID, "error", claimed.Error)
			continue
		}
		if claimed.RowsAffected == 0 {
			continue
		}
		w.sendLocation(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_LocationCommand{LocationCommand: &pluginv1.LocationCommand{
			Radio: uint32(cmd.RadioID), Id: cmd.ID, Action: cmd.Action, ExpiresAt: cmd.ExpiresAt.UnixMilli(), //nolint:gosec // owned 24-bit radio ID
		}}})
	}
}

func (w *watcher) finishLocationSession() {
	err := w.s.db.Model(&models.LocationCommand{}).
		Where("session = ? AND state IN ?", w.session, []string{"queued", "pending"}).
		Updates(map[string]any{"state": "failed", "detail": "Locator disconnected before receiving a reply",
			"expires_at": w.s.clock.Now().Add(30 * time.Second)}).Error
	if err != nil {
		slog.Error("Locator: cannot finish manual requests", "error", err)
	}
}

func (w *watcher) storeLocation(s *pluginv1.LocationStatus, ranges []models.PluginRange) {
	if w.p.Builtin != "locator" || s.GetRadio() == 0 || len(s.GetRequestId()) > 16 || len(s.GetDetail()) > 120 {
		return
	}
	if s.GetService() != 0 && !inRanges(ranges, uint(s.GetService())) {
		return
	}
	err := w.s.db.Transaction(func(tx *gorm.DB) error {
		// Lock the plugin row so a superseded stream cannot overwrite its replacement.
		var current models.Plugin
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "session", "enabled").First(&current, w.p.ID).Error; err != nil {
			return err
		}
		if !current.Enabled || current.Session != w.session {
			return nil
		}
		owner, ok, err := models.PositionOwner(tx, uint(s.GetRadio()))
		if err != nil || !ok {
			return err
		}
		currentOwner, owned, err := models.RadioIDOwner(tx, uint(s.GetRadio()))
		if err != nil || !owned || currentOwner != owner {
			return err
		}
		if s.GetCommandId() != "" {
			now := w.s.clock.Now()
			fields := map[string]any{"state": s.GetState(), "detail": s.GetDetail()}
			if s.GetState() == "pending" {
				fields["sent_at"] = time.UnixMilli(s.GetSentAt())
				fields["expires_at"] = now.Add(time.Minute)
			} else {
				fields["expires_at"] = now.Add(30 * time.Second)
			}
			return tx.Model(&models.LocationCommand{}).
				Where("radio_id = ? AND user_id = ? AND id = ? AND session = ? AND state IN ?",
					s.GetRadio(), owner, s.GetCommandId(), w.session, []string{"queued", "pending"}).
				Updates(fields).Error
		}
		payload, err := json.Marshal(s)
		if err != nil {
			return err
		}
		row := models.LocationSubscription{RadioID: uint(s.GetRadio()), UserID: owner, Snapshot: payload}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
	})
	if err != nil {
		slog.Error("Locator: cannot persist LRRP status", "radio", s.GetRadio(), "error", err)
	}
}
