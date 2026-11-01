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
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// RepeaterEvent is an entry of the connection log of a repeater: one kind
// of event from one address, Count times from FirstAt to LastAt.
type RepeaterEvent struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	RepeaterID uint      `json:"-" gorm:"not null;index:idx_repeater_events_repeater_last,priority:1"`
	FirstAt    time.Time `json:"first_at" gorm:"not null"`
	LastAt     time.Time `json:"last_at" gorm:"not null;index:idx_repeater_events_repeater_last,priority:2;index"`
	Count      uint      `json:"count" gorm:"not null;default:1"`
	Kind       string    `json:"kind" gorm:"size:32;not null"`
	Address    string    `json:"address" gorm:"size:64;not null;default:''"`
	Detail     string    `json:"detail" gorm:"size:255;not null;default:''"`
}

func (RepeaterEvent) TableName() string {
	return "repeater_events"
}

// RepeaterEventWindow is how long the connection log keeps an entry after
// its last event.
const RepeaterEventWindow = 24 * time.Hour

// NoteRepeaterEvent adds count events of the kind, address and detail of
// ev at ev.LastAt to the connection log of ev.RepeaterID. It folds them
// into the newest entry of the repeater when that entry is the same event,
// and reports false for a repeater that does not exist.
func NoteRepeaterEvent(db *gorm.DB, ev RepeaterEvent, count uint) (bool, error) {
	if count == 0 {
		count = 1
	}
	at := ev.LastAt.UTC()
	noted := false
	err := db.Transaction(func(tx *gorm.DB) error {
		exists, err := RepeaterIDExists(tx, ev.RepeaterID)
		if err != nil || !exists {
			return err
		}
		var last RepeaterEvent
		err = tx.Where("repeater_id = ?", ev.RepeaterID).Order("last_at DESC").Order("id DESC").Limit(1).Take(&last).Error
		switch {
		case err == nil && last.Kind == ev.Kind && last.Address == ev.Address && last.Detail == ev.Detail:
			upd := map[string]any{"count": gorm.Expr("count + ?", count)}
			if at.After(last.LastAt) {
				upd["last_at"] = at
			}
			if err := tx.Model(&RepeaterEvent{}).Where("id = ?", last.ID).Updates(upd).Error; err != nil {
				return fmt.Errorf("could not fold a connection log entry: %w", err)
			}
		case err == nil || errors.Is(err, gorm.ErrRecordNotFound):
			first := ev.FirstAt.UTC()
			if first.IsZero() || first.After(at) {
				first = at
			}
			row := RepeaterEvent{RepeaterID: ev.RepeaterID, FirstAt: first, LastAt: at, Count: count, Kind: ev.Kind, Address: ev.Address, Detail: ev.Detail}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("could not add a connection log entry: %w", err)
			}
		default:
			return fmt.Errorf("could not read the connection log: %w", err)
		}
		noted = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("repeater %d: %w", ev.RepeaterID, err)
	}
	return noted, nil
}

// ListRepeaterEvents returns the entries of the connection log of the
// repeater whose last event is after since, newest first.
func ListRepeaterEvents(db *gorm.DB, repeaterID uint, since time.Time) ([]RepeaterEvent, error) {
	var out []RepeaterEvent
	err := db.Where("repeater_id = ? AND last_at > ?", repeaterID, since.UTC()).Order("last_at DESC").Order("id DESC").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the connection log of repeater %d: %w", repeaterID, err)
	}
	return out, nil
}

// LastRepeaterProblem returns the newest entry of the connection log of
// the repeater after since whose kind is in kinds.
func LastRepeaterProblem(db *gorm.DB, repeaterID uint, since time.Time, kinds []string) (RepeaterEvent, bool, error) {
	var out []RepeaterEvent
	err := db.Where("repeater_id = ? AND last_at > ? AND kind IN ?", repeaterID, since.UTC(), kinds).Order("last_at DESC").Order("id DESC").Limit(1).Find(&out).Error
	if err != nil {
		return RepeaterEvent{}, false, fmt.Errorf("could not read the connection log of repeater %d: %w", repeaterID, err)
	}
	if len(out) == 0 {
		return RepeaterEvent{}, false, nil
	}
	return out[0], true, nil
}

// PurgeRepeaterEvents deletes the entries of the connection logs whose
// last event is before cut, and returns how many.
func PurgeRepeaterEvents(db *gorm.DB, cut time.Time) (int, error) {
	res := db.Where("last_at < ?", cut.UTC()).Delete(&RepeaterEvent{})
	if res.Error != nil {
		return 0, fmt.Errorf("could not purge the connection logs: %w", res.Error)
	}
	return int(res.RowsAffected), nil
}
