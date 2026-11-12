// SPDX-License-Identifier: AGPL-3.0-or-later

package models

import (
	"crypto/rand"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const LocationCommandLifetime = 10 * time.Minute

// LocationSubscription holds the latest periodic LRRP evidence for the current owner.
type LocationSubscription struct {
	RadioID  uint `gorm:"primaryKey;autoIncrement:false"`
	UserID   uint `gorm:"index"`
	Snapshot []byte
}

// LocationCommand holds the most recent manual operation for a radio.
type LocationCommand struct {
	RadioID   uint       `gorm:"primaryKey;autoIncrement:false" json:"-"`
	UserID    uint       `gorm:"index" json:"-"`
	ID        string     `json:"-"`
	Session   string     `gorm:"not null;default:'';index:idx_location_command_dispatch,priority:1" json:"-"`
	Action    string     `json:"action"`
	State     string     `gorm:"index:idx_location_command_dispatch,priority:2" json:"state"`
	Detail    string     `json:"detail,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
	ExpiresAt time.Time  `gorm:"index:idx_location_command_dispatch,priority:3" json:"-"`
}

func QueueLocationCommand(db *gorm.DB, radio, owner uint, action string, now time.Time) (LocationCommand, bool, error) {
	cmd := LocationCommand{
		RadioID: radio, UserID: owner, ID: rand.Text(), Action: action,
		State: "queued", CreatedAt: now, ExpiresAt: now.Add(LocationCommandLifetime),
	}
	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "radio_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"user_id", "id", "session", "action", "state", "detail", "created_at", "sent_at", "expires_at"}),
		Where: clause.Where{Exprs: []clause.Expression{clause.Expr{
			SQL: "location_commands.expires_at <= ? OR location_commands.user_id <> ?", Vars: []any{now, owner},
		}}},
	}).Create(&cmd)
	return cmd, result.RowsAffected == 1, result.Error
}
