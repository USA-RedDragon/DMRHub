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

package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// HeldState is the state of a held text.
type HeldState string

// The HeldState values.
const (
	// HeldStateHeld waits for the radio to be heard.
	HeldStateHeld HeldState = "held"
	// HeldStateSending is being sent by the replica in ClaimedBy.
	HeldStateSending HeldState = "sending"
	// HeldStateDelivered was sent, and acknowledged when the radio asks
	// for a response.
	HeldStateDelivered HeldState = "delivered"
	// HeldStateExpired was not delivered in the time that store and
	// forward keeps a text.
	HeldStateExpired HeldState = "expired"
	// HeldStateFailed was refused by the radio, or cannot be written in
	// the format of the radio.
	HeldStateFailed HeldState = "failed"
)

// HeldMessage is a private text that the hub keeps for a radio that it
// could not deliver to, for the user who owns the radio and turned store
// and forward on.
type HeldMessage struct {
	ID        uint      `gorm:"primarykey"`
	MessageID uint      `gorm:"not null;uniqueIndex"`
	Message   *Message  `gorm:"foreignKey:MessageID;constraint:OnDelete:CASCADE"`
	RadioID   uint      `gorm:"not null;index:idx_held_messages_radio_state,priority:1"`
	UserID    uint      `gorm:"not null;index:idx_held_messages_user_state,priority:1"`
	State     HeldState `gorm:"size:16;not null;index:idx_held_messages_radio_state,priority:2;index:idx_held_messages_user_state,priority:2"`
	Attempts  uint      `gorm:"not null;default:0"`
	HeldAt    time.Time `gorm:"not null;index"`
	DoneAt    *time.Time
	Response  CallResponse `gorm:"size:8"`
	ClaimedBy string       `gorm:"size:64"`
	ClaimedAt *time.Time
}

// TableName returns the table of the held texts.
func (HeldMessage) TableName() string {
	return "held_messages"
}

// BeforeSave keeps the times in UTC.
func (h *HeldMessage) BeforeSave(*gorm.DB) error {
	h.HeldAt = h.HeldAt.UTC()
	if h.DoneAt != nil {
		t := h.DoneAt.UTC()
		h.DoneAt = &t
	}
	if h.ClaimedAt != nil {
		t := h.ClaimedAt.UTC()
		h.ClaimedAt = &t
	}
	return nil
}

// SetStoreForward turns store and forward of the user on or off. Turning
// it off ends the texts held for the user's radios as expired.
func SetStoreForward(db *gorm.DB, userID uint, enabled bool, now time.Time) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE users SET store_forward = ? WHERE id = ?", enabled, userID).Error; err != nil {
			return fmt.Errorf("could not set store and forward of %d: %w", userID, err)
		}
		if enabled {
			return nil
		}
		if err := tx.Model(&HeldMessage{}).Where("user_id = ? AND state = ?", userID, HeldStateHeld).
			Updates(map[string]any{"state": HeldStateExpired, "done_at": now.UTC()}).Error; err != nil {
			return fmt.Errorf("could not end the held texts of %d: %w", userID, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store and forward: %w", err)
	}
	return nil
}
